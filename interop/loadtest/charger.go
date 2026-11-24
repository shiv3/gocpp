package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type charger struct {
	g       *gen
	id      string
	mute    bool
	tx      bool
	flapper bool
	dup     bool
	canary  bool
	shadow  bool // second client of a duplicate-id charger (not counted in g.up)
	seq     atomic.Int64
	rng     *rand.Rand

	// canary state
	hbStart  atomic.Int64 // unix nano of in-flight heartbeat, 0 if none
	canaryUp atomic.Bool
	maxRTT   atomic.Int64 // ns, reset by sampler
}

func newCharger(g *gen, id string) *charger {
	h := int64(0)
	for _, b := range []byte(id) {
		h = h*131 + int64(b)
	}
	return &charger{g: g, id: id, rng: rand.New(rand.NewSource(h))}
}

func (c *charger) runShadow() {
	s := newCharger(c.g, c.id)
	s.shadow, s.dup, s.tx, s.mute = true, true, c.tx, c.mute
	s.rng = rand.New(rand.NewSource(c.rng.Int63()))
	s.run()
}

func (c *charger) sleep(d time.Duration) bool {
	select {
	case <-time.After(d):
		return true
	case <-c.g.ctx.Done():
		return false
	}
}

func (c *charger) jit(max time.Duration) time.Duration {
	return time.Duration(c.rng.Int63n(int64(max) + 1))
}

func (c *charger) run() {
	cfg := c.g.cfg
	attempt := 0
	for c.g.ctx.Err() == nil {
		up := c.session()
		var d time.Duration
		switch {
		case c.flapper:
			d = time.Second + c.jit(2*time.Second)
		case c.dup:
			d = 3*time.Second + c.jit(5*time.Second)
		case up:
			attempt = 0
			d = c.jit(cfg.ReconnJitter0)
		default:
			attempt++
			m := cfg.ReconnBase << uint(min(attempt, 10))
			if m > cfg.ReconnCap || m <= 0 {
				m = cfg.ReconnCap
			}
			d = c.jit(m)
		}
		if !c.sleep(d) {
			return
		}
	}
}

type sess struct {
	c       *charger
	conn    *websocket.Conn
	ctx     context.Context
	cancel  context.CancelFunc
	mu      sync.Mutex
	pending map[string]chan []json.RawMessage
}

// session runs one connection; it reports whether BootNotification was accepted.
func (c *charger) session() (up bool) {
	g := c.g
	cap_.attempts.Add(1)
	dctx, dcancel := context.WithTimeout(g.ctx, 30*time.Second)
	conn, _, err := websocket.Dial(dctx, "ws://"+g.chargerAddr()+"/ocpp/"+c.id, &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6"}, HTTPClient: g.httpc,
	})
	dcancel()
	if err != nil {
		g.met.connErr(classifyErr(err))
		return false
	}
	conn.SetReadLimit(1 << 20)
	sctx, scancel := context.WithCancel(g.ctx)
	s := &sess{c: c, conn: conn, ctx: sctx, cancel: scancel, pending: map[string]chan []json.RawMessage{}}
	defer func() { scancel(); conn.CloseNow() }()
	go s.readLoop()
	if p := g.cfg.WSPing; p > 0 {
		// A timer, not a goroutine per charger: one short goroutine per ping.
		var pt *time.Timer
		pt = time.AfterFunc(c.jit(p)+time.Second, func() {
			if sctx.Err() != nil {
				return
			}
			pctx, pcancel := context.WithTimeout(sctx, 10*time.Second)
			_ = conn.Ping(pctx)
			pcancel()
			pt.Reset(p)
		})
		defer pt.Stop()
	}

	boot := fmt.Sprintf(`{"chargePointVendor":"LT","chargePointModel":"M1","chargePointSerialNumber":"%s"}`, c.id)
	res, err := s.call("BootNotification", boot)
	if err != nil {
		return false
	}
	var br struct{ Status string }
	if len(res) > 0 {
		_ = json.Unmarshal(res[0], &br)
	}
	if br.Status != "Accepted" {
		return false
	}
	up = true
	if c.canary {
		c.canaryUp.Store(true)
		defer c.canaryUp.Store(false)
	} else if !c.shadow {
		g.up.Add(1)
		defer g.up.Add(-1)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for k := 0; k <= g.cfg.Connectors; k++ {
		if _, err := s.call("StatusNotification", fmt.Sprintf(`{"connectorId":%d,"errorCode":"NoError","status":"Available","timestamp":"%s"}`, k, now)); err != nil {
			return
		}
	}
	if c.canary {
		s.canaryLoop()
		return
	}
	// flapper: drop the connection after a few seconds
	var flap <-chan time.Time
	if c.flapper {
		flap = time.After(3*time.Second + c.jit(5*time.Second))
	}
	hbPeriod := func() time.Duration {
		j := (c.rng.Float64()*2 - 1) * g.cfg.HBJitter
		return time.Duration(float64(g.cfg.HB) * (1 + j) * 1000 / float64(max(g.speed.Load(), 1)))
	}
	hb := time.NewTimer(c.jit(g.cfg.HB))
	defer hb.Stop()
	var txTimer *time.Timer
	var txC <-chan time.Time
	if c.tx {
		txTimer = time.NewTimer(c.jit(g.cfg.TxCycle))
		txC = txTimer.C
		defer txTimer.Stop()
	}
	for {
		select {
		case <-sctx.Done():
			return
		case <-flap:
			if c.rng.Intn(2) == 0 {
				conn.CloseNow()
			} else {
				conn.Close(websocket.StatusNormalClosure, "flap")
			}
			return
		case <-hb.C:
			if _, err := s.call("Heartbeat", `{}`); err != nil {
				return
			}
			hb.Reset(hbPeriod())
		case <-txC:
			if err := s.transaction(); err != nil {
				return
			}
			txTimer.Reset(g.cfg.TxCycle - g.cfg.TxLen + c.jit(g.cfg.TxCycle/4))
		}
	}
}

func (s *sess) transaction() error {
	c, cfg := s.c, s.c.g.cfg
	tag := "TAG" + c.id
	conn := 1 + c.rng.Intn(cfg.Connectors)
	if _, err := s.call("Authorize", fmt.Sprintf(`{"idTag":"%s"}`, tag)); err != nil {
		return err
	}
	ts := func() string { return time.Now().UTC().Format(time.RFC3339) }
	res, err := s.call("StartTransaction", fmt.Sprintf(`{"connectorId":%d,"idTag":"%s","meterStart":0,"timestamp":"%s"}`, conn, tag, ts()))
	if err != nil {
		return err
	}
	var sr struct {
		TransactionId int64 `json:"transactionId"`
	}
	if len(res) > 0 {
		_ = json.Unmarshal(res[0], &sr)
	}
	if _, err := s.call("StatusNotification", fmt.Sprintf(`{"connectorId":%d,"errorCode":"NoError","status":"Charging","timestamp":"%s"}`, conn, ts())); err != nil {
		return err
	}
	end := time.Now().Add(cfg.TxLen)
	wh := 0
	for time.Now().Before(end) {
		select {
		case <-time.After(time.Duration(float64(cfg.Meter) * 1000 / float64(max(s.c.g.speed.Load(), 1)))):
		case <-s.ctx.Done():
			return s.ctx.Err()
		}
		wh += 100
		mv := fmt.Sprintf(`{"connectorId":%d,"transactionId":%d,"meterValue":[{"timestamp":"%s","sampledValue":[{"value":"%d","measurand":"Energy.Active.Import.Register","unit":"Wh"}]}]}`, conn, sr.TransactionId, ts(), wh)
		if _, err := s.call("MeterValues", mv); err != nil {
			return err
		}
	}
	if _, err := s.call("StopTransaction", fmt.Sprintf(`{"transactionId":%d,"meterStop":%d,"timestamp":"%s","idTag":"%s"}`, sr.TransactionId, wh, ts(), tag)); err != nil {
		return err
	}
	_, err = s.call("StatusNotification", fmt.Sprintf(`{"connectorId":%d,"errorCode":"NoError","status":"Available","timestamp":"%s"}`, conn, ts()))
	return err
}

// canaryLoop sends a Heartbeat every second and tracks the round trip.
func (s *sess) canaryLoop() {
	c := s.c
	for {
		c.hbStart.Store(time.Now().UnixNano())
		t0 := time.Now()
		_, err := s.call("Heartbeat", `{}`)
		rtt := time.Since(t0)
		c.hbStart.Store(0)
		for {
			o := c.maxRTT.Load()
			if int64(rtt) <= o || c.maxRTT.CompareAndSwap(o, int64(rtt)) {
				break
			}
		}
		if err != nil {
			return
		}
		select {
		case <-time.After(time.Second):
		case <-s.ctx.Done():
			return
		}
	}
}

var errClosed = fmt.Errorf("closed")
var errTimeout = fmt.Errorf("timeout")

// call sends a CALL and waits for its result. Latency is recorded on success.
func (s *sess) call(action, payload string) ([]json.RawMessage, error) {
	c := s.c
	id := c.id + "-" + strconv.FormatInt(c.seq.Add(1), 36)
	ch := make(chan []json.RawMessage, 1)
	s.mu.Lock()
	s.pending[id] = ch
	s.mu.Unlock()
	defer func() { s.mu.Lock(); delete(s.pending, id); s.mu.Unlock() }()
	t0 := time.Now()
	wctx, wc := context.WithTimeout(s.ctx, 15*time.Second)
	err := s.conn.Write(wctx, websocket.MessageText, []byte(`[2,"`+id+`","`+action+`",`+payload+`]`))
	wc()
	if err != nil {
		if !c.canary {
			c.g.met.reqErr(action, "write")
		}
		return nil, errClosed
	}
	tm := time.NewTimer(c.g.cfg.CallTimeout)
	defer tm.Stop()
	select {
	case r := <-ch:
		d := time.Since(t0)
		c.g.msgs.Add(1)
		if !c.canary {
			c.g.met.req(action, d)
		}
		if r == nil {
			if !c.canary {
				c.g.met.reqErr(action, "callerror")
			}
			return nil, fmt.Errorf("callerror")
		}
		return r, nil
	case <-tm.C:
		if !c.canary {
			c.g.met.reqErr(action, "timeout")
		}
		return nil, errTimeout
	case <-s.ctx.Done():
		if !c.canary {
			c.g.met.reqErr(action, "closed")
		}
		return nil, errClosed
	}
}

func (s *sess) readLoop() {
	defer s.cancel()
	for {
		_, data, err := s.conn.Read(s.ctx)
		if err != nil {
			return
		}
		var f []json.RawMessage
		if json.Unmarshal(data, &f) != nil || len(f) < 3 {
			continue
		}
		var typ int
		_ = json.Unmarshal(f[0], &typ)
		var id string
		_ = json.Unmarshal(f[1], &id)
		switch typ {
		case 2: // CSMS call
			s.c.g.met.csmsCallSeen(s.c.mute)
			if s.c.mute {
				continue
			}
			var action string
			if len(f) > 2 {
				_ = json.Unmarshal(f[2], &action)
			}
			body := `{"status":"Accepted"}`
			if action == "GetConfiguration" {
				body = `{"configurationKey":[],"unknownKey":[]}`
			}
			wctx, wc := context.WithTimeout(s.ctx, 15*time.Second)
			_ = s.conn.Write(wctx, websocket.MessageText, []byte(`[3,"`+id+`",`+body+`]`))
			wc()
		case 3, 4:
			s.mu.Lock()
			ch := s.pending[id]
			s.mu.Unlock()
			if ch != nil {
				if typ == 3 {
					ch <- f[2:]
				} else {
					ch <- nil
				}
			}
		}
	}
}

// chargerAddr is where a charger connects: the server, or a random gateway.
func (g *gen) chargerAddr() string {
	if g.topo != nil {
		return g.topo.pickWS()
	}
	return g.cfg.Addr
}
