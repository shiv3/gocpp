// r02: call timeouts of mute charge points wedge the dispatcher's pump while
// healthy charge points are answering.
//
// M mute charge points (connected, reading, never answering a CSMS call) and
// K healthy ones. The CSMS calls the healthy ones in a steady loop and each
// mute one once, paced (never more than K+1 calls in flight at the sender, so
// the burst deadlock of r01 is excluded). When the mute calls time out, the
// pump goroutine completes them itself and signals itself on a one-slot
// channel that the healthy answers also fill.
//
// Expected with a sound design: M timeouts reach the application, the healthy
// calls and the canary keep being answered.
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
	m := flag.Int("mute", 300, "mute charge points")
	k := flag.Int("healthy", 8, "healthy charge points called in a loop")
	timeout := flag.Duration("timeout", 2*time.Second, "call timeout (library default 30 s; shortened to keep the run short)")
	flag.Parse()
	cs := h.StartCS(h.Opt{CallTimeout: *timeout})
	fmt.Printf("library: %s\nmute: %d, healthy: %d, call timeout: %v\n", h.Variant, *m, *k, *timeout)

	trigger := func() ocpp.Request {
		return remotetrigger.NewTriggerMessageRequest(remotetrigger.MessageTrigger("Heartbeat"))
	}
	var healthyOK, timeouts atomic.Int64
	for i := 0; i < *k; i++ {
		cp := h.MustDial(cs.Port, fmt.Sprintf("ok-%02d", i))
		go cp.AnswerCalls(`{"status":"Accepted"}`, 0)
		go func() { // one call at a time per healthy charge point
			for {
				ch := make(chan error, 1)
				if err := cs.SendRequestAsync(cp.ID, trigger(), func(_ ocpp.Response, err error) { ch <- err }); err != nil {
					time.Sleep(10 * time.Millisecond)
					continue
				}
				if <-ch == nil {
					healthyOK.Add(1)
				}
			}
		}()
	}
	var mute []*h.CP
	for i := 0; i < *m; i++ {
		cp := h.MustDial(cs.Port, fmt.Sprintf("mute-%04d", i))
		mute = append(mute, cp)
		go cp.AnswerCalls("", 0)
	}
	time.Sleep(300 * time.Millisecond)
	fmt.Printf("healthy calls answered before the mute calls: %d\n", healthyOK.Load())
	sendDone := make(chan struct{})
	go func() {
		for _, cp := range mute {
			_ = cs.SendRequestAsync(cp.ID, trigger(), func(_ ocpp.Response, err error) {
				if err != nil {
					timeouts.Add(1)
				}
			})
			time.Sleep(time.Millisecond)
		}
		close(sendDone)
	}()
	note := func() string {
		return fmt.Sprintf("(healthy calls answered %d, mute timeouts delivered %d/%d)", healthyOK.Load(), timeouts.Load(), *m)
	}
	time.Sleep(*timeout)
	ok, failed := h.CanaryLoop(cs.Port, 5, 3*time.Second, note)
	before := healthyOK.Load()
	time.Sleep(2 * time.Second)
	progress := healthyOK.Load() - before
	fmt.Printf("healthy calls answered in the last 2 s: %d\n", progress)
	fmt.Printf("\ngoroutines by state and top frames (server and test clients share this process):\n%s", h.GoroutineSummary(8))
	h.Result(failed > 0 || progress == 0 || timeouts.Load() < int64(*m),
		"mute timeouts delivered %d/%d; healthy calls answered in the last 2 s: %d; canary %d/%d", timeouts.Load(), *m, progress, ok, ok+failed)
}
