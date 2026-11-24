// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	ocpp2 "github.com/lorenzodonini/ocpp-go/ocpp2.0.1"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/provisioning"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// These tests cover the charge point (client) side, which is ocpp-go's own code
// hardened by this module: the cases that panic or hang on upstream.

func within(t *testing.T, what string, d time.Duration, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); f() }()
	select {
	case <-done:
	case <-time.After(d):
		t.Fatalf("%s did not return within %v", what, d)
	}
}

func plainCP(t *testing.T, c *central, id string, h *cpHandler) ocpp16.ChargePoint {
	cp := ocpp16.NewChargePoint(id, nil, nil)
	cp.SetCoreHandler(h)
	cp.SetRemoteTriggerHandler(h)
	startMu.Lock()
	err := cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", c.port))
	startMu.Unlock()
	if err != nil {
		t.Fatalf("cp %s start: %v", id, err)
	}
	return cp
}

func TestClientStopIsIdempotent(t *testing.T) {
	c := startCentral(t)
	cp := plainCP(t, c, "CP-STOP2", &cpHandler{})
	if _, err := cp.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	within(t, "Stop x3", 10*time.Second, func() { cp.Stop(); cp.Stop(); cp.Stop() })
	if cp.IsConnected() {
		t.Fatal("still connected")
	}
}

func TestClientStopBeforeStart(t *testing.T) {
	c := startCentral(t)
	cp := ocpp16.NewChargePoint("CP-NEVER", nil, nil)
	cp.SetCoreHandler(&cpHandler{})
	within(t, "Stop before Start", 5*time.Second, func() { cp.Stop(); cp.Stop() })
	// ... and the client can still be started afterwards.
	startMu.Lock()
	err := cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", c.port))
	startMu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cp.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	cp.Stop()
}

func TestClientSendAfterStopReturnsError(t *testing.T) {
	c := startCentral(t)
	cp := plainCP(t, c, "CP-AFTER", &cpHandler{})
	cp.Stop()
	within(t, "SendRequest after Stop", 5*time.Second, func() {
		if _, err := cp.Heartbeat(); err == nil {
			t.Error("Heartbeat after Stop must fail")
		}
		if err := cp.SendRequestAsync(core.NewHeartbeatRequest(), func(ocpp.Response, error) {}); err == nil {
			t.Error("SendRequestAsync after Stop must fail")
		}
	})
}

// TestClientSendDuringStop: requests racing with Stop neither hang nor panic.
func TestClientSendDuringStop(t *testing.T) {
	c := startCentral(t)
	for round := 0; round < 5; round++ {
		cp := plainCP(t, c, fmt.Sprintf("CP-RACE%d", round), &cpHandler{})
		var wg sync.WaitGroup
		for i := 0; i < 8; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 20; j++ {
					_ = cp.SendRequestAsync(core.NewHeartbeatRequest(), func(ocpp.Response, error) {})
					_, _ = cp.Heartbeat()
				}
			}()
		}
		time.Sleep(5 * time.Millisecond)
		within(t, "Stop during sends", 15*time.Second, cp.Stop)
		within(t, "senders", 15*time.Second, wg.Wait)
	}
}

type panicCPHandler struct{ cpHandler }

func (h *panicCPHandler) OnRemoteStartTransaction(*core.RemoteStartTransactionRequest) (*core.RemoteStartTransactionConfirmation, error) {
	panic("charge point handler bug")
}

// TestClientHandlerPanicIsContained: a panic in the charge point's handler is
// answered with InternalError, reported on Errors(), and the client keeps working.
func TestClientHandlerPanicIsContained(t *testing.T) {
	c := startCentral(t)
	cp := plainCP(t, c, "CP-PANIC", &cpHandler{})
	t.Cleanup(cp.Stop)
	cp.SetCoreHandler(&panicCPHandler{})
	errs := cp.Errors()
	got := make(chan error, 1)
	if err := c.cs.SendRequestAsync("CP-PANIC", core.NewRemoteStartTransactionRequest("TAG"), func(_ ocpp.Response, err error) { got <- err }); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-got:
		oe, ok := err.(*ocpp.Error)
		if !ok || oe.Code != ocppj.InternalError {
			t.Fatalf("central system got %v, want InternalError", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no answer to the request whose handler panicked")
	}
	select {
	case err := <-errs:
		t.Logf("reported: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("panic not reported on Errors()")
	}
	if _, err := cp.Heartbeat(); err != nil {
		t.Fatalf("client unusable after a handler panic: %v", err)
	}
}

// TestClientCallbackPanicIsContained: a panic in a SendRequestAsync callback.
func TestClientCallbackPanicIsContained(t *testing.T) {
	c := startCentral(t)
	cp := plainCP(t, c, "CP-CBPANIC", &cpHandler{})
	t.Cleanup(cp.Stop)
	errs := cp.Errors()
	if err := cp.SendRequestAsync(core.NewHeartbeatRequest(), func(ocpp.Response, error) { panic("callback bug") }); err != nil {
		t.Fatal(err)
	}
	select {
	case <-errs:
	case <-time.After(5 * time.Second):
		t.Fatal("panic not reported on Errors()")
	}
	if _, err := cp.Heartbeat(); err != nil {
		t.Fatalf("client unusable after a callback panic: %v", err)
	}
}

// flapServer answers every call with a Heartbeat result and drops all
// connections every period; it counts accepted connections.
func flapServer(t *testing.T, period time.Duration) (port int, accepted *atomic.Int64) {
	t.Helper()
	var mu sync.Mutex
	conns := map[*websocket.Conn]bool{}
	accepted = new(atomic.Int64)
	up := websocket.Upgrader{Subprotocols: []string{"ocpp1.6"}}
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		accepted.Add(1)
		mu.Lock()
		conns[c] = true
		mu.Unlock()
		for {
			_, b, err := c.ReadMessage()
			if err != nil {
				return
			}
			if len(b) > 12 && b[1] == '2' {
				end := 4
				for end < len(b) && b[end] != '"' {
					end++
				}
				_ = c.WriteMessage(websocket.TextMessage, []byte(`[3,`+string(b[3:end+1])+`,{"currentTime":"2026-11-24T00:00:00Z"}]`))
			}
		}
	})}
	go srv.Serve(l)
	stop := make(chan struct{})
	go func() {
		tk := time.NewTicker(period)
		defer tk.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tk.C:
				mu.Lock()
				for c := range conns {
					_ = c.UnderlyingConn().Close()
					delete(conns, c)
				}
				mu.Unlock()
			}
		}
	}()
	t.Cleanup(func() { close(stop); _ = srv.Close() })
	return l.Addr().(*net.TCPAddr).Port, accepted
}

func fastReconnectCP(id string) ocpp16.ChargePoint {
	c := ws.NewClient()
	cfg := ws.NewClientTimeoutConfig()
	cfg.RetryBackOffWaitMinimum = 20 * time.Millisecond
	cfg.RetryBackOffRandomRange = 0
	c.SetTimeoutConfig(cfg)
	return ocpp16.NewChargePoint(id, ocppj.NewClient(id, c, nil, nil, core.Profile), c)
}

// TestClientKeepsServingWhileConnectionsDrop (upstream r11-B, scaled down): a few
// charge points send Heartbeats while the server drops every connection. They
// must keep exchanging messages (upstream's dispatcher can deadlock in Pause).
func TestClientKeepsServingWhileConnectionsDrop(t *testing.T) {
	port, _ := flapServer(t, 100*time.Millisecond)
	var ok atomic.Int64
	var cps []ocpp16.ChargePoint
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		cp := fastReconnectCP(fmt.Sprintf("cp-%d", i))
		cps = append(cps, cp)
		startMu.Lock()
		err := cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", port))
		startMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = cp.SendRequestAsync(core.NewHeartbeatRequest(), func(r ocpp.Response, err error) {
					if err == nil {
						ok.Add(1)
					}
				})
				time.Sleep(2 * time.Millisecond)
			}
		}()
	}
	time.Sleep(1500 * time.Millisecond)
	before := ok.Load()
	time.Sleep(1500 * time.Millisecond)
	if ok.Load() == before {
		t.Fatalf("no Heartbeat answered during the last 1.5 s (total %d)", before)
	}
	close(stop)
	within(t, "senders", 5*time.Second, wg.Wait)
	for _, cp := range cps {
		within(t, "Stop", 10*time.Second, cp.Stop)
	}
}

// TestClientStopDuringReconnect: Stop while the client is reconnecting must
// leave no connection behind.
func TestClientStopDuringReconnect(t *testing.T) {
	port, accepted := flapServer(t, 50*time.Millisecond)
	for i := 0; i < 10; i++ {
		cp := fastReconnectCP(fmt.Sprintf("cp-r%d", i))
		startMu.Lock()
		err := cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", port))
		startMu.Unlock()
		if err != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Duration(40+i*7) * time.Millisecond)
		within(t, "Stop", 10*time.Second, cp.Stop)
		within(t, "Stop again", 10*time.Second, cp.Stop)
	}
	time.Sleep(200 * time.Millisecond)
	n := accepted.Load()
	time.Sleep(500 * time.Millisecond)
	if accepted.Load() != n {
		t.Fatalf("a stopped client reconnected (%d -> %d connections)", n, accepted.Load())
	}
}

// TestWsClientWriteAfterStop: Write on a stopped ws client fails, it does not panic or block.
func TestWsClientWriteAfterStop(t *testing.T) {
	port, _ := flapServer(t, time.Hour)
	c := ws.NewClient()
	c.SetRequestedSubProtocol("ocpp1.6")
	if err := c.Start(fmt.Sprintf("ws://127.0.0.1:%d/x", port)); err != nil {
		t.Fatal(err)
	}
	c.Stop()
	c.Stop()
	within(t, "Write after Stop", 5*time.Second, func() {
		for i := 0; i < 5; i++ {
			if err := c.Write([]byte("x")); err == nil {
				// a write queued before the close completed is fine; once closed it must fail
				time.Sleep(50 * time.Millisecond)
			}
		}
		if err := c.Write([]byte("x")); err == nil {
			t.Error("Write after Stop must fail")
		}
	})
}

// TestClient201Lifecycle: the same guarantees for the OCPP 2.0.1 charging station.
func TestClient201Lifecycle(t *testing.T) {
	port, _ := flapServer(t, time.Hour)
	never := ocpp2.NewChargingStation("CS-NEVER", nil, nil)
	within(t, "Stop before Start", 5*time.Second, func() { never.Stop(); never.Stop() })

	cs := ocpp2.NewChargingStation("CS-1", nil, nil)
	if err := cs.Start(fmt.Sprintf("ws://127.0.0.1:%d", port)); err != nil {
		t.Fatal(err)
	}
	within(t, "double Stop", 10*time.Second, func() { cs.Stop(); cs.Stop() })
	within(t, "request after Stop", 5*time.Second, func() {
		if _, err := cs.BootNotification(provisioning.BootReasonPowerUp, "m", "v"); err == nil {
			t.Error("BootNotification after Stop must fail")
		}
	})
}
