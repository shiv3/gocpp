// r05: the server reads an inbound frame of any size into memory.
//
// One client streams a single text frame of -size MiB (a syntactically valid
// DataTransfer call) and the program records the peak resident memory of the
// process. The client side streams from a small buffer, so the growth is the
// server's.
package main

import (
	"flag"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"time"

	"github.com/gorilla/websocket"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func vm(key string) int {
	b, _ := os.ReadFile("/proc/self/status")
	m := regexp.MustCompile(key + `:\s+(\d+) kB`).FindSubmatch(b)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(string(m[1]))
	return n / 1024
}

func main() {
	size := flag.Int("size", 64, "frame size in MiB")
	flag.Parse()
	cs := h.StartCS(h.Opt{})
	fmt.Printf("library: %s\nframe: %d MiB (one DataTransfer call)\n", h.Variant, *size)
	cp := h.MustDial(cs.Port, "big")
	runtime.GC()
	before := vm("VmRSS")
	w, err := cp.Conn.NextWriter(websocket.TextMessage)
	if err != nil {
		panic(err)
	}
	_, _ = w.Write([]byte(`[2,"big-1","DataTransfer",{"vendorId":"v","data":"`))
	chunk := make([]byte, 64<<10)
	for i := range chunk {
		chunk[i] = 'a'
	}
	var werr error
	for i := 0; i < *size*16 && werr == nil; i++ {
		_, werr = w.Write(chunk)
	}
	if werr == nil {
		_, _ = w.Write([]byte(`"}]`))
		werr = w.Close()
	}
	_, raw, rerr := cp.Read(60 * time.Second)
	if len(raw) > 100 {
		raw = raw[:100] + "..."
	}
	peak := vm("VmHWM")
	fmt.Printf("client write error: %v\nserver reply: %q (read error: %v)\n", werr, raw, rerr)
	fmt.Printf("process RSS before: %d MiB, peak RSS: %d MiB (%.1fx the frame)\n", before, peak, float64(peak-before)/float64(*size))
	accepted := rerr == nil && len(raw) > 0
	h.Result(accepted && peak-before > *size, "%d MiB frame: accepted and answered=%v; peak RSS grew by %d MiB", *size, accepted, peak-before)
}
