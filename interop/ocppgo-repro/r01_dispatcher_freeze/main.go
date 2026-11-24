// r01: a burst of CSMS calls deadlocks the server dispatcher.
//
// N HEALTHY charge points connect (each answers a TriggerMessage at once).
// The CSMS sends one TriggerMessage to each from a plain loop, as an
// application does after a deploy or a fleet-wide reconnect. Then a canary
// charge point sends Heartbeat once per second.
//
// Expected with a sound design: all N calls are answered, the canary is
// answered in milliseconds.
package main

import (
	"flag"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	n := flag.Int("n", 200, "charge points")
	flag.Parse()
	cs := h.StartCS(h.Opt{}) // library defaults (call timeout 30 s)
	fmt.Printf("library: %s\nhealthy charge points: %d\n", h.Variant, *n)

	var cps []*h.CP
	for i := 0; i < *n; i++ {
		cp := h.MustDial(cs.Port, fmt.Sprintf("cp-%04d", i))
		cps = append(cps, cp)
		go cp.AnswerCalls(`{"status":"Accepted"}`, 0)
	}
	time.Sleep(300 * time.Millisecond)

	var sent, answered atomic.Int64
	done := make(chan struct{})
	go func() {
		for _, cp := range cps {
			_ = cs.SendRequestAsync(cp.ID, remotetrigger.NewTriggerMessageRequest(remotetrigger.MessageTrigger("Heartbeat")),
				func(_ ocpp.Response, err error) {
					if err == nil {
						answered.Add(1)
					}
				})
			sent.Add(1)
		}
		close(done)
	}()
	select {
	case <-done:
		fmt.Printf("send loop returned: %d calls sent\n", sent.Load())
	case <-time.After(5 * time.Second):
		fmt.Printf("send loop STILL BLOCKED inside SendRequestAsync after 5 s: %d of %d calls sent\n", sent.Load(), *n)
	}
	note := func() string {
		return fmt.Sprintf("(calls sent %d, answered %d of %d)", sent.Load(), answered.Load(), *n)
	}
	ok, failed := h.CanaryLoop(cs.Port, 4, 3*time.Second, note)
	fmt.Printf("\ngoroutines by state and top frames (server and test clients share this process):\n%s", h.GoroutineSummary(7))
	h.Result(failed > 0 || answered.Load() < int64(*n), "%d/%d calls sent, %d answered; canary answered %d/%d Heartbeats", sent.Load(), *n, answered.Load(), ok, ok+failed)
}
