// r12: what one idle connection costs the server: goroutines by function.
// With -bridge the central system is given an application-side WsServer that
// wraps the library's *ws.Server and only delegates (what an app does when it
// adds its own auth, routing or TLS handling around the server). On the drop-in
// that is "bridge mode": the module does not own the sockets.
// (Memory per connection is taken from the load tests, which run the server
// in its own process; here clients and server share one process.)
package main

import (
	"flag"
	"fmt"
	"runtime"
	"time"

	"github.com/lorenzodonini/ocpp-go/ws"
	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

// appServer is the application's own WsServer; every method is promoted from
// the wrapped server.
type appServer struct{ ws.WsServer }

func main() {
	n := flag.Int("n", 2000, "idle connections")
	bridge := flag.Bool("bridge", false, "give the central system an app-side WsServer wrapper around the library's ws.Server")
	flag.Parse()
	o := h.Opt{}
	if *bridge {
		o.Wrap = func(inner *ws.Server) ws.WsServer { return &appServer{inner} }
	}
	cs := h.StartCS(o)
	base := runtime.NumGoroutine()
	var keep []*h.CP
	for i := 0; i < *n; i++ {
		keep = append(keep, h.MustDial(cs.Port, fmt.Sprintf("cp-%05d", i)))
	}
	time.Sleep(time.Second)
	g := runtime.NumGoroutine()
	mode := "direct"
	if *bridge {
		mode = "app-side WsServer wrapper"
	}
	fmt.Printf("library: %s (%s)\n%d idle connections (the raw test clients run no goroutines)\n", h.Variant, mode, *n)
	fmt.Printf("goroutines: %d before, %d now = %.2f per connection\n%s", base, g, float64(g-base)/float64(*n), h.GoroutineSummary(6))
	runtime.KeepAlive(keep)
	h.Result(float64(g-base)/float64(*n) > 1.5, "%.2f goroutines per idle connection", float64(g-base)/float64(*n))
}
