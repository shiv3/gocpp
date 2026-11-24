// r11: the charge point (client) side of ocpp-go crashes its process.
//
//	A. Stop called twice (for example from a signal handler and a deferred
//	   cleanup).
//	B. charge points keep sending Heartbeats while the server drops their
//	   connections (deploy, load balancer idle timeout, network flap).
//
// Each part runs in a child process; the parent reports whether it died.
// The client code is ocpp-go's own in both variants (the drop-in module
// replaces only the server side).
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

// flappingServer answers every call and drops all connections every period.
func flappingServer(period time.Duration) int {
	var mu sync.Mutex
	conns := map[*websocket.Conn]bool{}
	up := websocket.Upgrader{Subprotocols: []string{"ocpp1.6"}}
	l, _ := net.Listen("tcp", ":0")
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		mu.Lock()
		conns[c] = true
		mu.Unlock()
		for {
			_, b, err := c.ReadMessage()
			if err != nil {
				return
			}
			if len(b) > 12 && b[1] == '2' { // [2,"id",...
				end := 4
				for end < len(b) && b[end] != '"' {
					end++
				}
				_ = c.WriteMessage(websocket.TextMessage, []byte(`[3,`+string(b[3:end+1])+`,{"currentTime":"2026-11-24T00:00:00Z"}]`))
			}
		}
	}))
	go func() {
		for range time.Tick(period) {
			mu.Lock()
			for c := range conns {
				_ = c.UnderlyingConn().Close() // abrupt drop, no close frame
				delete(conns, c)
			}
			mu.Unlock()
		}
	}()
	return l.Addr().(*net.TCPAddr).Port
}

func newCP(id string) ocpp16.ChargePoint {
	c := ws.NewClient()
	cfg := ws.NewClientTimeoutConfig()
	cfg.RetryBackOffWaitMinimum = 50 * time.Millisecond // reconnect quickly (default 10 s + jitter)
	cfg.RetryBackOffRandomRange = 0
	c.SetTimeoutConfig(cfg)
	return ocpp16.NewChargePoint(id, ocppj.NewClient(id, c, nil, nil, core.Profile), c)
}

func main() {
	n := flag.Int("n", 200, "charge points in part B")
	dur := flag.Duration("d", 40*time.Second, "how long part B runs at most")
	flag.Parse()
	switch {
	case h.IsChild("A"):
		port := flappingServer(time.Hour)
		cp := newCP("cp-a")
		if err := cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", port)); err != nil {
			panic(err)
		}
		fmt.Println("A. connected; calling Stop twice")
		cp.Stop()
		cp.Stop()
		fmt.Println("A. second Stop returned")
		return
	case h.IsChild("B"):
		port := flappingServer(300 * time.Millisecond)
		var ok atomic.Int64
		t0 := time.Now()
		var cps []ocpp16.ChargePoint
		for i := 0; i < *n; i++ {
			cp := newCP(fmt.Sprintf("cp-%04d", i))
			cps = append(cps, cp)
			go func() {
				for cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", port)) != nil {
					time.Sleep(50 * time.Millisecond)
				}
				for {
					_ = cp.SendRequestAsync(core.NewHeartbeatRequest(), func(r ocpp.Response, err error) {
						if err == nil {
							ok.Add(1)
						}
					})
					time.Sleep(2 * time.Millisecond)
				}
			}()
		}
		last, stalled := int64(0), 0
		for {
			time.Sleep(5 * time.Second)
			conn := 0
			for _, cp := range cps {
				if cp.IsConnected() {
					conn++
				}
			}
			cur := ok.Load()
			fmt.Printf("B. t=%2.0fs process alive; Heartbeats answered so far %d (+%d in 5 s); charge points reporting IsConnected: %d/%d\n", time.Since(t0).Seconds(), cur, cur-last, conn, *n)
			if cur == last {
				stalled++
				if stalled == 2 {
					fmt.Printf("B. no Heartbeat answered for 10 s although the server is up; client goroutines:\n%s", h.GoroutineSummary(8))
				}
			}
			last = cur
		}
	}
	fmt.Printf("library: %s (client code: ocpp-go's own in both variants)\n", h.Variant)
	diedA, errA, headA := h.RunChild("A", 10*time.Second)
	fmt.Printf("A. double Stop: process died=%v (%v)\n%s\n", diedA, errA, headA)
	fmt.Printf("B. %d charge points sending Heartbeat every 2 ms; the server drops every connection every 300 ms\n", *n)
	t0 := time.Now()
	diedB, errB, headB := h.RunChild("B", *dur)
	fmt.Printf("B. process died=%v after %.1f s (%v)\n%s", diedB, time.Since(t0).Seconds(), errB, headB)
	h.Result(diedA || diedB, "client process crashed: double Stop=%v, sending while connections drop=%v (see the B lines: the clients may stop exchanging messages without crashing)", diedA, diedB)
}
