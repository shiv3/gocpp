// r06: a charge point that reconnects after a half-open drop is locked out.
//
// "cp1" connects. Its link then dies without a FIN reaching the server (power
// cut, NAT timeout, LTE handover): the old socket stays open on the server
// and nothing more arrives on it. The charge point reconnects with the same
// id every 5 s. The program prints what each attempt gets and how long it
// takes until the charge point is served again. All timeouts are the
// library defaults.
package main

import (
	"fmt"
	"runtime"
	"time"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	cs := h.StartCS(h.Opt{})
	fmt.Printf("library: %s\n", h.Variant)
	old := h.MustDial(cs.Port, "cp1")
	d, err := old.Heartbeat("hb-old", 3*time.Second)
	fmt.Printf("first connection: Heartbeat answered in %v (%v); the link now goes silent without a close\n", d.Round(10*time.Microsecond), err)
	t0 := time.Now()
	served := time.Duration(-1)
	refused := 0
	for attempt := 1; time.Since(t0) < 90*time.Second; attempt++ {
		time.Sleep(5 * time.Second)
		cp, err := h.Dial(cs.Port, "cp1")
		if err != nil {
			refused++
			fmt.Printf("t=%4.1fs attempt %d: handshake refused: %v\n", time.Since(t0).Seconds(), attempt, err)
			continue
		}
		_, err = cp.Heartbeat(fmt.Sprintf("hb-%d", attempt), 3*time.Second)
		if err == nil {
			served = time.Since(t0)
			fmt.Printf("t=%4.1fs attempt %d: served (Heartbeat answered)\n", served.Seconds(), attempt)
			break
		}
		refused++
		fmt.Printf("t=%4.1fs attempt %d: upgraded, then closed by the server: %v\n", time.Since(t0).Seconds(), attempt, err)
		cp.Conn.Close()
	}
	runtime.KeepAlive(old) // the old socket stays open, silent
	h.Result(refused > 0, "reconnecting charge point refused %d time(s); served again after %.0f s", refused, served.Seconds())
}
