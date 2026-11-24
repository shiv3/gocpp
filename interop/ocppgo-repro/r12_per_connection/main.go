// r12: what one idle connection costs the server: goroutines by function.
// (Memory per connection is taken from the load tests, which run the server
// in its own process; here clients and server share one process.)
package main

import (
	"flag"
	"fmt"
	"runtime"
	"time"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	n := flag.Int("n", 2000, "idle connections")
	flag.Parse()
	cs := h.StartCS(h.Opt{})
	base := runtime.NumGoroutine()
	var keep []*h.CP
	for i := 0; i < *n; i++ {
		keep = append(keep, h.MustDial(cs.Port, fmt.Sprintf("cp-%05d", i)))
	}
	time.Sleep(time.Second)
	g := runtime.NumGoroutine()
	fmt.Printf("library: %s\n%d idle connections (the raw test clients run no goroutines)\n", h.Variant, *n)
	fmt.Printf("goroutines: %d before, %d now = %.2f per connection\n%s", base, g, float64(g-base)/float64(*n), h.GoroutineSummary(6))
	runtime.KeepAlive(keep)
	h.Result(float64(g-base)/float64(*n) > 1.5, "%.2f goroutines per idle connection", float64(g-base)/float64(*n))
}
