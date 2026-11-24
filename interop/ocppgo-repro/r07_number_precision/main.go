// r07: integers are decoded through float64 (interface{} -> re-marshal ->
// typed struct), so large ids are silently changed or rejected.
//
// A charge point sends StopTransaction with a 64-bit transactionId (vendors
// such as Alfen do: upstream issue #188). The handler prints what it received.
package main

import (
	"fmt"
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	got := make(chan int, 1)
	cs := h.StartCS(h.Opt{Handler: &h.Handler{Stop: func(id string, r *core.StopTransactionRequest) { got <- r.TransactionId }}})
	fmt.Printf("library: %s\n", h.Variant)
	cp := h.MustDial(cs.Port, "cp1")
	changed, rejected := 0, 0
	for i, id := range []string{"1234567", "9007199254740993", "1152921504606846977", "9223372036854775807"} {
		_ = cp.Send(fmt.Sprintf(`[2,"m%d","StopTransaction",{"transactionId":%s,"meterStop":10,"timestamp":"2026-11-24T00:00:00Z"}]`, i, id))
		_, raw, err := cp.Read(3 * time.Second)
		select {
		case v := <-got:
			mark := "same"
			if fmt.Sprint(v) != id {
				mark = "CHANGED SILENTLY"
				changed++
			}
			fmt.Printf("wire transactionId %-20s -> handler saw %-20d %s; reply %s\n", id, v, mark, raw)
		case <-time.After(500 * time.Millisecond):
			rejected++
			fmt.Printf("wire transactionId %-20s -> handler NOT called; reply %s (err %v)\n", id, raw, err)
		}
	}
	h.Result(changed+rejected > 0, "%d id(s) silently changed, %d valid 64-bit id(s) rejected", changed, rejected)
}
