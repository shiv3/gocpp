// r08: responses are handed to callbacks by position (one FIFO per charge
// point), not by message id. Three situations, all with the public API:
//
//	A. invalid answer: the charge point answers a call with a payload that
//	   fails validation. What does the application hear, and when? What does
//	   the charge point receive? How long is the next call to it delayed?
//	B. late answer racing the call timeout: the answer arrives at the moment
//	   the timeout fires. Which callback gets it?
//	C. no disconnect handler registered (it is optional in the API): a call
//	   is pending when the charge point drops; it reconnects and the CSMS
//	   sends a different call. Which callback gets the answer?
//
// Callbacks registered through SendRequestAsync record the type they were
// given, so a misdelivery is counted instead of crashing. Part C2 repeats C
// with the typed API (cs.ChangeAvailability / cs.GetConfiguration), where the
// library's own wrapper type-asserts the response.
package main

import (
	"flag"
	"fmt"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func name(r ocpp.Response) string {
	if r == nil {
		return "<nil>"
	}
	return r.GetFeatureName()
}

func main() {
	timeout := flag.Duration("timeout", 2*time.Second, "call timeout (library default 30 s)")
	raceN := flag.Int("race-n", 300, "charge points in part B")
	flag.Parse()
	fmt.Printf("library: %s\ncall timeout: %v\n", h.Variant, *timeout)
	mis := 0

	// ---------- A: invalid answer ----------
	{
		cs := h.StartCS(h.Opt{CallTimeout: *timeout, Before: func(cs *h.CS) {
			cs.SetChargePointDisconnectedHandler(func(cp ocpp16cp) {})
		}})
		cp := h.MustDial(cs.Port, "a1")
		time.Sleep(200 * time.Millisecond)
		t0 := time.Now()
		type res struct {
			what string
			at   time.Duration
		}
		ch := make(chan res, 2)
		_ = cs.SendRequestAsync("a1", core.NewGetConfigurationRequest([]string{"k"}), func(r ocpp.Response, err error) {
			ch <- res{fmt.Sprintf("GetConfiguration callback: response=%s err=%v", name(r), err), time.Since(t0)}
		})
		_ = cs.SendRequestAsync("a1", core.NewChangeAvailabilityRequest(1, core.AvailabilityTypeOperative), func(r ocpp.Response, err error) {
			ch <- res{fmt.Sprintf("ChangeAvailability callback: response=%s err=%v", name(r), err), time.Since(t0)}
		})
		arr, raw, _ := cp.Read(3 * time.Second)
		fmt.Println("\nA. charge point received:", raw)
		// invalid: "key" is required in each configurationKey
		_ = cp.Send(`[3,` + string(arr[1]) + `,{"configurationKey":[{"readonly":true,"value":"v"}]}]`)
		fmt.Println("A. charge point answered at once with an invalid payload (configurationKey without the required key)")
		go func() {
			for {
				arr, raw, err := cp.Read(3 * *timeout)
				if err != nil {
					return
				}
				fmt.Printf("A. t=%.2fs charge point received: %s\n", time.Since(t0).Seconds(), raw)
				if len(arr) > 2 && string(arr[0]) == "2" {
					_ = cp.Send(`[3,` + string(arr[1]) + `,{"status":"Accepted"}]`)
				}
			}
		}()
		for i := 0; i < 2; i++ {
			select {
			case r := <-ch:
				fmt.Printf("A. t=%.2fs application: %s\n", r.at.Seconds(), r.what)
			case <-time.After(3 * *timeout):
				fmt.Println("A. application: a callback was never called")
			}
		}
		cs.Stop()
	}

	// ---------- B: late answer racing the timeout ----------
	{
		cs := h.StartCS(h.Opt{CallTimeout: *timeout, Before: func(cs *h.CS) {
			cs.SetChargePointDisconnectedHandler(func(cp ocpp16cp) {})
		}})
		var libErrs atomic.Int64
		go func() {
			for range cs.Errors() {
				libErrs.Add(1)
			}
		}()
		var wrong, right, timeouts atomic.Int64
		var wg sync.WaitGroup
		var mu sync.Mutex
		example := ""
		var bcps []*h.CP
		for i := 0; i < *raceN; i++ {
			bcps = append(bcps, h.MustDial(cs.Port, fmt.Sprintf("b%04d", i)))
		}
		time.Sleep(300 * time.Millisecond)
		sendErrs := 0
		for _, cp := range bcps {
			id := cp.ID
			// answers GetConfiguration just around the timeout, everything else at once
			go func() {
				first := true
				for {
					arr, _, err := cp.Read(5 * *timeout)
					if err != nil {
						return
					}
					if len(arr) < 3 || string(arr[0]) != "2" {
						continue
					}
					if first {
						first = false
						time.Sleep(*timeout - 1500*time.Microsecond + time.Duration(rand.Intn(3000))*time.Microsecond)
						_ = cp.Send(`[3,` + string(arr[1]) + `,{"configurationKey":[]}]`)
						continue
					}
					_ = cp.Send(`[3,` + string(arr[1]) + `,{"status":"Accepted"}]`)
				}
			}()
			wg.Add(2)
			check := func(want string) func(ocpp.Response, error) {
				return func(r ocpp.Response, err error) {
					defer wg.Done()
					switch {
					case r == nil:
						timeouts.Add(1)
					case r.GetFeatureName() != want:
						wrong.Add(1)
						mu.Lock()
						if example == "" {
							example = fmt.Sprintf("charge point %s: the callback of %s was given a %T", id, want, r)
						}
						mu.Unlock()
					default:
						right.Add(1)
					}
				}
			}
			if err := cs.SendRequestAsync(id, core.NewGetConfigurationRequest(nil), check("GetConfiguration")); err != nil {
				sendErrs++
				wg.Done()
			}
			time.Sleep(2 * time.Millisecond) // paced: excludes the burst deadlock of r01
			if err := cs.SendRequestAsync(id, core.NewChangeAvailabilityRequest(1, core.AvailabilityTypeOperative), check("ChangeAvailability")); err != nil {
				sendErrs++
				wg.Done()
			}
		}
		done := make(chan struct{})
		go func() { wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(4 * *timeout):
			fmt.Println("\nB. not every callback was called within 4 timeouts")
		}
		never := 2*int64(*raceN) - int64(sendErrs) - right.Load() - timeouts.Load() - wrong.Load()
		fmt.Printf("\nB. %d charge points, GetConfiguration answered within +-1.5 ms of the timeout, then ChangeAvailability:\n", *raceN)
		fmt.Printf("B. callbacks given the right response: %d, a timeout/error: %d, the response of ANOTHER request: %d; never called: %d; library errors (\"no handler available\"): %d; send errors: %d\n",
			right.Load(), timeouts.Load(), wrong.Load(), never, libErrs.Load(), sendErrs)
		if example != "" {
			fmt.Println("B. example:", example)
		}
		mis += int(wrong.Load())
	}

	// ---------- C: no disconnect handler ----------
	cs := h.StartCS(h.Opt{CallTimeout: 30 * time.Second})
	{
		cp := h.MustDial(cs.Port, "c1")
		time.Sleep(200 * time.Millisecond)
		got := make(chan string, 2)
		_ = cs.SendRequestAsync("c1", core.NewChangeAvailabilityRequest(1, core.AvailabilityTypeOperative), func(r ocpp.Response, err error) {
			got <- fmt.Sprintf("the ChangeAvailability callback (sent before the drop) was called with response=%s err=%v", name(r), err)
		})
		_, _, _ = cp.Read(2 * time.Second)
		cp.Conn.Close() // drops without answering
		time.Sleep(300 * time.Millisecond)
		cp = h.MustDial(cs.Port, "c1")
		go cp.AnswerCalls(`{"configurationKey":[]}`, 0)
		time.Sleep(200 * time.Millisecond)
		if err := cs.SendRequestAsync("c1", core.NewGetConfigurationRequest(nil), func(r ocpp.Response, err error) {
			got <- fmt.Sprintf("the GetConfiguration callback (sent after the reconnect) was called with response=%s err=%v", name(r), err)
		}); err != nil {
			fmt.Println("C. send error:", err)
		}
		fmt.Println()
		for i := 0; i < 2; i++ {
			select {
			case s := <-got:
				fmt.Println("C.", s)
				if i == 0 && len(s) > 24 && s[4:22] == "ChangeAvailability" && !contains(s, "response=<nil>") {
					mis++
				}
			case <-time.After(2 * time.Second):
				fmt.Println("C. (the other callback was not called within 2 s)")
			}
		}
	}
	fmt.Printf("\nmisdelivered responses counted in B and C: %d\n", mis)
	fmt.Printf("RESULT-SO-FAR [%s]: misdelivered=%d (C2 follows; if the library's typed wrapper receives the wrong type the process dies here)\n\n", h.Variant, mis)

	// ---------- C2: the same with the typed API ----------
	cp := h.MustDial(cs.Port, "c2")
	time.Sleep(200 * time.Millisecond)
	_ = cs.ChangeAvailability("c2", func(c *core.ChangeAvailabilityConfirmation, err error) {
		fmt.Printf("C2. ChangeAvailability callback: %v %v\n", c, err)
	}, 1, core.AvailabilityTypeOperative)
	_, _, _ = cp.Read(2 * time.Second)
	cp.Conn.Close()
	time.Sleep(300 * time.Millisecond)
	cp = h.MustDial(cs.Port, "c2")
	go cp.AnswerCalls(`{"configurationKey":[]}`, 0)
	time.Sleep(200 * time.Millisecond)
	_ = cs.GetConfiguration("c2", func(c *core.GetConfigurationConfirmation, err error) {
		fmt.Printf("C2. GetConfiguration callback: %v %v\n", c, err)
	}, nil)
	time.Sleep(2 * time.Second)
	h.Result(mis > 0, "misdelivered responses: %d; the typed API survived C2", mis)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
