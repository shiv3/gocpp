// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shiv3/gocpp/core/observability"

	"github.com/lorenzodonini/ocpp-go/compat"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// recMetrics records what the compat module reports.
type recMetrics struct {
	observability.NoOp
	mu       sync.Mutex
	conns    int
	calls    map[string]int
	upgrades map[string]int
	closed   []string
}

func newRecMetrics() *recMetrics {
	return &recMetrics{calls: map[string]int{}, upgrades: map[string]int{}}
}
func (m *recMetrics) ConnectionCount(_ string, d int) { m.mu.Lock(); m.conns += d; m.mu.Unlock() }
func (m *recMetrics) CallCompleted(_, action, dir string, _ time.Duration, status string) {
	m.mu.Lock()
	m.calls[action+"/"+dir+"/"+status]++
	m.mu.Unlock()
}
func (m *recMetrics) SessionClosed(_ string, _ time.Duration, code, cause string) {
	m.mu.Lock()
	m.closed = append(m.closed, code+"/"+cause)
	m.mu.Unlock()
}
func (m *recMetrics) UpgradeResult(r string) { m.mu.Lock(); m.upgrades[r]++; m.mu.Unlock() }
func (m *recMetrics) get(f func() int) int   { m.mu.Lock(); defer m.mu.Unlock(); return f() }

func startOps(t *testing.T, adm compat.Admission, m observability.Metrics) (*central, *ws.Server) {
	t.Helper()
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("admission, drain and metrics are features of the gocpp runtime")
	}
	srv := ws.NewServer()
	endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
	if m != nil && !compat.SetObservability(endpoint, m, nil, nil) {
		t.Fatal("SetObservability: not the gocpp runtime")
	}
	if !compat.SetAdmission(srv, adm) {
		t.Fatal("SetAdmission failed")
	}
	c := &central{h: &csHandler{}, port: freePort(t), srv: srv}
	c.cs = ocpp16.NewCentralSystem(endpoint, c.srv)
	c.cs.SetCoreHandler(c.h)
	c.cs.SetNewChargePointHandler(func(ocpp16.ChargePointConnection) { c.connects.Add(1) })
	c.cs.SetChargePointDisconnectedHandler(func(ocpp16.ChargePointConnection) { c.drops.Add(1) })
	srv.AddHttpHandler("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	srv.AddHttpHandler("/ready", compat.ReadyHandler(srv).ServeHTTP)
	srv.SetCheckOriginHandler(func(r *http.Request) bool { return true })
	go c.cs.Start(c.port, "/{ws}")
	waitFor(t, "listening", func() bool {
		resp, err := healthGet(c.port)
		if err == nil {
			resp.Body.Close()
		}
		return err == nil
	})
	t.Cleanup(srv.Stop)
	return c, srv
}

func dialCP(c *central, id string) (*websocket.Conn, *http.Response, error) {
	return websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/%s", c.port, id),
		http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}})
}

func readyStatus(t *testing.T, c *central) int {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/ready", c.port))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestAdmissionMaxConnectionsAndSoftCap(t *testing.T) {
	m := newRecMetrics()
	c, srv := startOps(t, compat.Admission{MaxConnections: 2, SoftMaxConnections: 1}, m)
	compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins) // the replacement below needs it
	a, _, err := dialCP(c, "A")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, _, err := dialCP(c, "B")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	waitFor(t, "2 connected", func() bool { return compat.Connections(srv) == 2 })

	// Over the cap: refused before the handshake.
	_, resp, err := dialCP(c, "C")
	if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("third connection: err=%v resp=%v, want 503", err, resp)
	}
	if resp.Header.Get("Retry-After") == "" {
		t.Error("no Retry-After on the refusal")
	}
	// A reconnect under a connected id replaces the old connection and takes no slot.
	a2, _, err := dialCP(c, "A")
	if err != nil {
		t.Fatalf("replacement refused: %v", err)
	}
	defer a2.Close()
	waitFor(t, "replacement closed the old one", func() bool { return c.drops.Load() >= 1 })

	// Soft cap: ready until the share is reached.
	if got := readyStatus(t, c); got != http.StatusServiceUnavailable {
		t.Errorf("ready at the soft cap = %d, want 503", got)
	}
	compat.SetSoftMaxConnections(srv, 0)
	if got := readyStatus(t, c); got != http.StatusOK {
		t.Errorf("ready without soft cap = %d, want 200", got)
	}
	if st := compat.Stats(srv); st.RejectedFull != 1 {
		t.Errorf("stats %+v, want RejectedFull=1", st)
	}
	waitFor(t, "upgrade metrics", func() bool {
		return m.get(func() int { return m.upgrades["full"] }) == 1 && m.get(func() int { return m.upgrades["ok"] }) == 3
	})
	defer func() { m.mu.Lock(); t.Logf("closed: %v", m.closed); m.mu.Unlock() }()
	waitFor(t, "close metric of the replaced connection", func() bool {
		return m.get(func() int {
			n := 0
			for _, s := range m.closed {
				if s == "1008/replaced" {
					n++
				}
			}
			return n
		}) == 1
	})
}

func TestAdmissionAcceptRate(t *testing.T) {
	c, srv := startOps(t, compat.Admission{AcceptRate: 0.5, AcceptBurst: 1}, nil)
	a, _, err := dialCP(c, "A")
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	_, resp, err := dialCP(c, "B")
	if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("second upgrade within the rate: err=%v resp=%v, want 503", err, resp)
	}
	if st := compat.Stats(srv); st.RejectedRateLimit != 1 {
		t.Errorf("stats %+v", st)
	}
}

func TestDrainAndMetrics(t *testing.T) {
	m := newRecMetrics()
	c, srv := startOps(t, compat.Admission{}, m)
	var conns []*websocket.Conn
	closeCodes := make(chan int, 8)
	for i := 0; i < 4; i++ {
		cn, _, err := dialCP(c, fmt.Sprintf("D%d", i))
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, cn)
		go func(cn *websocket.Conn) {
			for {
				if _, _, err := cn.ReadMessage(); err != nil {
					if ce, ok := err.(*websocket.CloseError); ok {
						closeCodes <- ce.Code
					} else {
						closeCodes <- -1
					}
					return
				}
			}
		}(cn)
	}
	waitFor(t, "connected", func() bool { return compat.Connections(srv) == 4 })
	// A completed call in each direction shows up in the call metrics.
	if err := conns[0].WriteMessage(websocket.TextMessage, []byte(`[2,"h1","Heartbeat",{}]`)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "inbound call metric", func() bool {
		return m.get(func() int { return m.calls["Heartbeat/inbound/ok"] }) == 1
	})
	if got := m.get(func() int { return m.conns }); got != 4 {
		t.Errorf("connection gauge = %d, want 4", got)
	}
	if readyStatus(t, c) != http.StatusOK {
		t.Error("not ready before the drain")
	}

	var progress int
	done := make(chan error, 1)
	go func() {
		done <- compat.Drain(context.Background(), srv, compat.DrainOptions{
			Window: 5 * time.Second, Rate: 20, RetryAfter: 2 * time.Second,
			OnProgress: func(compat.DrainProgress) { progress++ },
		})
	}()
	for i := 0; i < 4; i++ {
		select {
		case code := <-closeCodes:
			if code != 1012 {
				t.Errorf("close code %d, want 1012", code)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the drain to close a connection")
		}
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Drain: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Drain did not return")
	}
	if progress == 0 {
		t.Error("OnProgress was never called")
	}
	if !compat.Draining(srv) || readyStatus(t, c) != http.StatusServiceUnavailable {
		t.Error("draining server must report not-ready")
	}
	_, resp, err := dialCP(c, "late")
	if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("upgrade while draining: err=%v resp=%v, want 503 with Retry-After", err, resp)
	}
	waitFor(t, "disconnect callbacks", func() bool { return c.drops.Load() == 4 })
	waitFor(t, "drain close metrics", func() bool {
		return m.get(func() int {
			n := 0
			for _, s := range m.closed {
				if s == "1012/drain" {
					n++
				}
			}
			return n
		}) == 4
	})
	if st := compat.Stats(srv); st.RejectedDraining != 1 {
		t.Errorf("stats %+v", st)
	}
	compat.Undrain(srv)
	again, _, err := dialCP(c, "late")
	if err != nil {
		t.Fatalf("after Undrain: %v", err)
	}
	again.Close()
}

// A second connection for a connected id: first wins by default, as ocpp-go
// v0.19.0 (the old one stays and the new one is closed with 1008 after the
// upgrade); with compat.DuplicateLastWins the old one is closed with 1008.
func TestDuplicatePolicy(t *testing.T) {
	closeCode := func(t *testing.T, c *websocket.Conn) int {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
		_, _, err := c.ReadMessage()
		if ce, ok := err.(*websocket.CloseError); ok {
			return ce.Code
		}
		t.Fatalf("expected a close frame, got %v", err)
		return 0
	}
	alive := func(t *testing.T, c *websocket.Conn) bool {
		t.Helper()
		_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		_, _, err := c.ReadMessage()
		_, closed := err.(*websocket.CloseError)
		return !closed
	}
	t.Run("last wins when asked", func(t *testing.T) {
		c, srv := startOps(t, compat.Admission{}, nil)
		if !compat.SetDuplicatePolicy(srv, compat.DuplicateLastWins) {
			t.Fatal("SetDuplicatePolicy failed")
		}
		first, _, err := dialCP(c, "dup")
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		second, _, err := dialCP(c, "dup")
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		if code := closeCode(t, first); code != websocket.ClosePolicyViolation {
			t.Fatalf("old connection closed with %d, want 1008", code)
		}
		if !alive(t, second) {
			t.Fatal("new connection was closed")
		}
	})
	t.Run("first wins by default", func(t *testing.T) {
		c, _ := startOps(t, compat.Admission{}, nil)
		first, _, err := dialCP(c, "dup")
		if err != nil {
			t.Fatal(err)
		}
		defer first.Close()
		waitFor(t, "first connected", func() bool { return c.connects.Load() == 1 })
		second, _, err := dialCP(c, "dup")
		if err != nil {
			t.Fatal(err)
		}
		defer second.Close()
		if code := closeCode(t, second); code != websocket.ClosePolicyViolation {
			t.Fatalf("new connection closed with %d, want 1008", code)
		}
		if !alive(t, first) {
			t.Fatal("old connection was closed")
		}
		if n := c.connects.Load(); n != 1 {
			t.Fatalf("application saw %d connects, want 1", n)
		}
	})
}
