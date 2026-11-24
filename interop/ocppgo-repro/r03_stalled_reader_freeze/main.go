// r03: ONE charge point that stops reading its socket freezes every other
// charge point (ws layer, independent of the dispatcher).
//
// "stalled" connects, sends three DataTransfer requests and then never reads
// (a half-open link, a hung firmware, or a hostile client). The handler
// answers each with a large payload, so the answers do not fit the socket
// buffers. A healthy canary sends Heartbeat once per second throughout.
//
// Expected with a sound design: the stalled connection is cut after the write
// timeout; nobody else notices.
package main

import (
	"flag"
	"fmt"
	"net"
	"runtime"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	writeWait := flag.Duration("write-wait", 3*time.Second, "server write timeout (library default 10 s; shortened to keep the run short)")
	size := flag.Int("size", 16<<20, "bytes of data in each DataTransfer answer")
	flag.Parse()
	big := strings.Repeat("x", *size)
	cfg := ws.NewServerTimeoutConfig()
	cfg.WriteWait = *writeWait
	cs := h.StartCS(h.Opt{WSTimeouts: &cfg, Handler: &h.Handler{
		DataTransfer: func(id string, r *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
			c := core.NewDataTransferConfirmation(core.DataTransferStatusAccepted)
			if id == "stalled" {
				c.Data = big
			}
			return c, nil
		}}})
	fmt.Printf("library: %s\nserver write timeout: %v, answer size: %d MiB\n", h.Variant, *writeWait, *size>>20)

	canary := h.MustDial(cs.Port, "canary")
	d, err := canary.Heartbeat("hb-before", 3*time.Second)
	fmt.Printf("canary Heartbeat before: %v %v\n", d.Round(10*time.Microsecond), err)

	// the stalled charge point: small receive buffer, never reads
	dialer := websocket.Dialer{Subprotocols: []string{"ocpp1.6"}, ReadBufferSize: 1024,
		NetDial: func(network, addr string) (net.Conn, error) {
			c, err := net.Dial(network, addr)
			if tc, ok := c.(*net.TCPConn); ok {
				_ = tc.SetReadBuffer(4096)
			}
			return c, err
		}}
	st, _, err := dialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/stalled", cs.Port), nil)
	if err != nil {
		panic(err)
	}
	t0 := time.Now()
	for i := 1; i <= 3; i++ {
		_ = st.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf(`[2,"dt-%d","DataTransfer",{"vendorId":"v"}]`, i)))
	}
	fmt.Println("stalled charge point sent 3 DataTransfer requests and stopped reading")

	ok, failed, newConnFailed := 0, 0, 0
	for i := 0; time.Since(t0) < *writeWait+8*time.Second; i++ {
		time.Sleep(time.Second)
		d, err := canary.Heartbeat(fmt.Sprintf("hb-%d", i), 2*time.Second)
		if err != nil {
			failed++
			fmt.Printf("t=%4.1fs canary Heartbeat: NO ANSWER within 2 s\n", time.Since(t0).Seconds())
			canary = nil
			if c2, err2 := h.Dial(cs.Port, fmt.Sprintf("canary-%d", i)); err2 == nil {
				canary = c2
			}
			if canary == nil {
				newConnFailed++
				fmt.Println("        and a new connection could not be opened")
				canary = h.MustDial(cs.Port, fmt.Sprintf("canary-b%d", i))
			}
			continue
		}
		ok++
		fmt.Printf("t=%4.1fs canary Heartbeat: answered in %v\n", time.Since(t0).Seconds(), d.Round(10*time.Microsecond))
	}
	fmt.Printf("\ngoroutines by state and top frames (server and test clients share this process):\n%s", h.GoroutineSummary(8))
	runtime.KeepAlive(st) // the stalled socket stays open (not closed, not read)
	h.Result(failed > 0, "canary answered %d/%d Heartbeats while one charge point was not reading", ok, ok+failed)
}
