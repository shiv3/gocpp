// Package h holds the helpers shared by the repro programs: an OCPP 1.6
// central system built on the public ocpp-go API, and a raw charge point
// (plain gorilla/websocket) whose behaviour the scenario scripts exactly.
package h

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// Handler is a minimal core profile handler. Hooks may be nil.
type Handler struct {
	Heartbeat    func(id string) (*core.HeartbeatConfirmation, error)
	DataTransfer func(id string, r *core.DataTransferRequest) (*core.DataTransferConfirmation, error)
	Stop         func(id string, r *core.StopTransactionRequest)
}

func (h *Handler) OnAuthorize(id string, r *core.AuthorizeRequest) (*core.AuthorizeConfirmation, error) {
	return core.NewAuthorizationConfirmation(types.NewIdTagInfo(types.AuthorizationStatusAccepted)), nil
}
func (h *Handler) OnBootNotification(id string, r *core.BootNotificationRequest) (*core.BootNotificationConfirmation, error) {
	return core.NewBootNotificationConfirmation(types.NewDateTime(time.Now()), 300, core.RegistrationStatusAccepted), nil
}
func (h *Handler) OnDataTransfer(id string, r *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	if h.DataTransfer != nil {
		return h.DataTransfer(id, r)
	}
	return core.NewDataTransferConfirmation(core.DataTransferStatusAccepted), nil
}
func (h *Handler) OnHeartbeat(id string, r *core.HeartbeatRequest) (*core.HeartbeatConfirmation, error) {
	if h.Heartbeat != nil {
		return h.Heartbeat(id)
	}
	return core.NewHeartbeatConfirmation(types.NewDateTime(time.Now())), nil
}
func (h *Handler) OnMeterValues(id string, r *core.MeterValuesRequest) (*core.MeterValuesConfirmation, error) {
	return core.NewMeterValuesConfirmation(), nil
}
func (h *Handler) OnStatusNotification(id string, r *core.StatusNotificationRequest) (*core.StatusNotificationConfirmation, error) {
	return core.NewStatusNotificationConfirmation(), nil
}
func (h *Handler) OnStartTransaction(id string, r *core.StartTransactionRequest) (*core.StartTransactionConfirmation, error) {
	return core.NewStartTransactionConfirmation(types.NewIdTagInfo(types.AuthorizationStatusAccepted), 1), nil
}
func (h *Handler) OnStopTransaction(id string, r *core.StopTransactionRequest) (*core.StopTransactionConfirmation, error) {
	if h.Stop != nil {
		h.Stop(id, r)
	}
	return core.NewStopTransactionConfirmation(), nil
}

// CS is a running central system.
type CS struct {
	ocpp16.CentralSystem
	WS       *ws.Server
	Endpoint *ocppj.Server
	Port     int
}

// Opt configures StartCS.
type Opt struct {
	CallTimeout time.Duration // 0: library default (30 s)
	WSTimeouts  *ws.ServerTimeoutConfig
	Handler     *Handler
	Before      func(cs *CS) // called before Start
	// Wrap, when set, hands the central system an application-side WsServer
	// that delegates to the library's own *ws.Server (r12 bridge mode).
	// CS.WS stays the inner server, which is where compat.Set* must be called.
	Wrap func(inner *ws.Server) ws.WsServer
}

// StartCS starts an OCPP 1.6 central system on a free port, the way the
// ocpp-go README does it, and waits until it accepts connections.
func StartCS(o Opt) *CS {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		panic(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	srv := ws.NewServer()
	if o.WSTimeouts != nil {
		srv.SetTimeoutConfig(*o.WSTimeouts)
	}
	var front ws.WsServer = srv
	if o.Wrap != nil {
		front = o.Wrap(srv)
	}
	endpoint := newEndpoint(front, o.CallTimeout) // build-tag specific (timeout_*.go)
	cs := &CS{CentralSystem: ocpp16.NewCentralSystem(endpoint, front), WS: srv, Endpoint: endpoint, Port: port}
	hd := o.Handler
	if hd == nil {
		hd = &Handler{}
	}
	cs.SetCoreHandler(hd)
	if ApplySafe(cs) {
		fmt.Println("opt-in safer behaviours of the drop-in: ON (last-wins duplicates, 1 MiB inbound limit, close frame on idle close)")
	}
	if o.Before != nil {
		o.Before(cs)
	}
	go cs.Start(port, "/{ws}")
	for i := 0; i < 200; i++ {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			return cs
		}
		time.Sleep(10 * time.Millisecond)
	}
	panic("server did not start")
}

// CP is a raw charge point connection.
type CP struct {
	ID   string
	Conn *websocket.Conn
	wmu  sync.Mutex
}

// Dial connects a raw OCPP 1.6 charge point.
func Dial(port int, id string) (*CP, error) {
	d := websocket.Dialer{Subprotocols: []string{"ocpp1.6"}, HandshakeTimeout: 10 * time.Second}
	c, _, err := d.Dial(fmt.Sprintf("ws://127.0.0.1:%d/%s", port, id), nil)
	if err != nil {
		return nil, err
	}
	return &CP{ID: id, Conn: c}, nil
}

func MustDial(port int, id string) *CP {
	cp, err := Dial(port, id)
	if err != nil {
		panic(fmt.Sprintf("dial %s: %v", id, err))
	}
	return cp
}

// Send writes a raw text frame.
func (c *CP) Send(s string) error {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.Conn.WriteMessage(websocket.TextMessage, []byte(s))
}

// Read returns the next frame as a decoded OCPP-J array, or an error after d.
func (c *CP) Read(d time.Duration) ([]json.RawMessage, string, error) {
	_ = c.Conn.SetReadDeadline(time.Now().Add(d))
	_, b, err := c.Conn.ReadMessage()
	if err != nil {
		return nil, "", err
	}
	var arr []json.RawMessage
	_ = json.Unmarshal(b, &arr)
	return arr, string(b), nil
}

// Heartbeat sends a Heartbeat call and reports how long the answer took.
func (c *CP) Heartbeat(id string, wait time.Duration) (time.Duration, error) {
	t0 := time.Now()
	if err := c.Send(`[2,"` + id + `","Heartbeat",{}]`); err != nil {
		return 0, err
	}
	for {
		arr, _, err := c.Read(wait - time.Since(t0))
		if err != nil {
			return time.Since(t0), err
		}
		if len(arr) >= 2 && string(arr[1]) == `"`+id+`"` {
			return time.Since(t0), nil
		}
	}
}

// GoroutineSummary groups the goroutines of this process by state and top
// frames (the same reduction as in the load test's freeze evidence) and
// returns the n largest groups.
func GoroutineSummary(n int) string {
	var buf bytes.Buffer
	_ = pprof.Lookup("goroutine").WriteTo(&buf, 2)
	groups := map[string]int{}
	for _, g := range strings.Split(buf.String(), "\n\n") {
		lines := strings.Split(strings.TrimSpace(g), "\n")
		if len(lines) < 2 || !strings.HasPrefix(lines[0], "goroutine ") {
			continue
		}
		state := lines[0][strings.Index(lines[0], "[")+1:]
		state = strings.SplitN(strings.TrimSuffix(state, "]:"), ",", 2)[0]
		var fr []string
		for _, l := range lines[1:] {
			if strings.HasPrefix(l, "\t") || strings.HasPrefix(l, "created by") {
				continue
			}
			f := l
			if i := strings.LastIndex(f, "("); i > 0 {
				f = f[:i]
			}
			if i := strings.LastIndex(f, "/"); i >= 0 {
				f = f[i+1:]
			}
			if strings.HasPrefix(f, "runtime.") || strings.HasPrefix(f, "sync.") || strings.HasPrefix(f, "internal/") {
				continue
			}
			fr = append(fr, f)
			if len(fr) == 3 {
				break
			}
		}
		groups[state+" | "+strings.Join(fr, " <- ")]++
	}
	type kv struct {
		k string
		v int
	}
	var s []kv
	for k, v := range groups {
		s = append(s, kv{k, v})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].v > s[j].v || (s[i].v == s[j].v && s[i].k < s[j].k) })
	var out strings.Builder
	for i, e := range s {
		if i >= n && !strings.Contains(e.k, "messagePump") { // always show the dispatcher pump
			continue
		}
		fmt.Fprintf(&out, "  %6d  %s\n", e.v, e.k)
	}
	return out.String()
}

// Result prints the verdict line every repro ends with and exits.
func Result(reproduced bool, format string, a ...any) {
	v := "NOT REPRODUCED"
	if reproduced {
		v = "REPRODUCED"
	}
	fmt.Printf("\nRESULT [%s]: %s: %s\n", Variant, v, fmt.Sprintf(format, a...))
	os.Exit(0)
}

// AnswerCalls reads frames and answers every CSMS call with the given payload
// after delay (nil payload: never answers = mute). It returns when the socket closes.
func (c *CP) AnswerCalls(payload string, delay time.Duration) {
	for {
		_, b, err := c.Conn.ReadMessage()
		if err != nil {
			return
		}
		var arr []json.RawMessage
		if json.Unmarshal(b, &arr) != nil || len(arr) < 3 || string(arr[0]) != "2" || payload == "" {
			continue
		}
		if delay > 0 {
			time.Sleep(delay)
		}
		_ = c.Send(`[3,` + string(arr[1]) + `,` + payload + `]`)
	}
}

// CanaryLoop sends n Heartbeats one second apart on a dedicated charge point
// and reports how many were answered within wait.
func CanaryLoop(port int, n int, wait time.Duration, note func() string) (ok, failed int) {
	canary := MustDial(port, "canary")
	for i := 0; i < n; i++ {
		d, err := canary.Heartbeat(fmt.Sprintf("hb-%d", i), wait)
		if err != nil {
			failed++
			fmt.Printf("canary Heartbeat %d: NO ANSWER within %v (%v) %s\n", i, wait, err, note())
			if c2, err2 := Dial(port, fmt.Sprintf("canary-%d", i)); err2 == nil {
				canary = c2 // the read deadline poisoned the socket
			} else {
				fmt.Println("   canary reconnect failed:", err2)
			}
			continue
		}
		ok++
		fmt.Printf("canary Heartbeat %d: answered in %v %s\n", i, d.Round(10*time.Microsecond), note())
		time.Sleep(time.Second)
	}
	return
}
