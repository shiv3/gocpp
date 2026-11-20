package benchmarks

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
)

// footprint is the server-side resource cost of N connections, per connection.
type footprint struct {
	HeapPerConn       float64 // runtime.MemStats.HeapAlloc delta / N, after a forced GC
	StackPerConn      float64 // runtime.MemStats.StackInuse delta / N
	GoroutinesPerConn float64 // runtime.NumGoroutine delta / N
}

func newHeartbeatServer(tb testing.TB) (*csms.Server, *httptest.Server) {
	tb.Helper()
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	err := csms.On(srv, profiles.Heartbeat, func(context.Context, *csms.Conn, messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{CurrentTime: time.Unix(0, 0).UTC()}, nil
	})
	if err != nil {
		tb.Fatal(err)
	}
	return srv, httptest.NewServer(srv.Handler())
}

// rawConn is a deliberately dumb WebSocket client: raw TCP, hand-written
// handshake and a single masked text frame. It keeps no goroutines and almost
// no heap per connection, so the measured delta is the server's cost.
type rawConn struct{ net.Conn }

func dialRaw(addr, id string) (rawConn, error) {
	c, err := net.Dial("tcp", addr)
	if err != nil {
		return rawConn{}, err
	}
	_, err = fmt.Fprintf(c, "GET /ocpp/%s HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: ocpp1.6\r\n\r\n", id)
	if err != nil {
		_ = c.Close()
		return rawConn{}, err
	}
	// The server sends nothing after the 101 until asked, so reading up to the
	// blank line with a temporary buffer leaves no per-connection allocation.
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 512)
	for !bytes.Contains(buf, []byte("\r\n\r\n")) {
		n, err := c.Read(tmp)
		if err != nil {
			_ = c.Close()
			return rawConn{}, err
		}
		buf = append(buf, tmp[:n]...)
	}
	resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(buf)), nil)
	if err != nil {
		_ = c.Close()
		return rawConn{}, fmt.Errorf("handshake response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		_ = c.Close()
		return rawConn{}, fmt.Errorf("handshake failed: status %d", resp.StatusCode)
	}
	return rawConn{c}, nil
}

// heartbeat sends one Heartbeat CALL and reads the reply frame header+payload.
func (c rawConn) heartbeat() error {
	msg := []byte(`[2,"1","Heartbeat",{}]`)
	fr := append([]byte{0x81, 0x80 | byte(len(msg)), 0, 0, 0, 0}, msg...)
	if _, err := c.Write(fr); err != nil {
		return err
	}
	_ = c.SetReadDeadline(time.Now().Add(10 * time.Second))
	var hdr [2]byte
	if err := readFull(c, hdr[:]); err != nil {
		return err
	}
	rest := make([]byte, int(hdr[1]&0x7f))
	if hdr[1]&0x7f >= 126 {
		return fmt.Errorf("unexpected long reply")
	}
	return readFull(c, rest)
}

func readFull(c net.Conn, b []byte) error {
	_, err := io.ReadFull(c, b)
	return err
}

type memSnap struct {
	heap, stack uint64
	goroutines  int
}

func snapMem() memSnap {
	// Two GCs so finalizers and sync.Pool victim caches are flushed.
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memSnap{heap: m.HeapAlloc, stack: m.StackInuse, goroutines: runtime.NumGoroutine()}
}

func perConn(after, before memSnap, n int) footprint {
	d := func(a, b uint64) float64 { return (float64(a) - float64(b)) / float64(n) }
	return footprint{
		HeapPerConn:       d(after.heap, before.heap),
		StackPerConn:      d(after.stack, before.stack),
		GoroutinesPerConn: float64(after.goroutines-before.goroutines) / float64(n),
	}
}

// measureFootprint opens n connections through the real csms server, lets them
// go idle, snapshots, sends one Heartbeat on each, and snapshots again.
func measureFootprint(tb testing.TB, n int) (idle, afterHB footprint) {
	tb.Helper()
	srv, ts := newHeartbeatServer(tb)
	defer ts.Close()
	addr := strings.TrimPrefix(ts.URL, "http://")

	before := snapMem()
	conns := make([]rawConn, 0, n)
	defer func() {
		for _, c := range conns {
			_ = c.Close()
		}
	}()
	for i := 0; i < n; i++ {
		c, err := dialRaw(addr, fmt.Sprintf("CP_%05d", i))
		if err != nil {
			tb.Fatalf("conn %d: %v", i, err)
		}
		conns = append(conns, c)
	}
	deadline := time.Now().Add(30 * time.Second)
	for i := 0; i < n; i++ {
		for {
			if _, ok := srv.Get(fmt.Sprintf("CP_%05d", i)); ok {
				break
			}
			if time.Now().After(deadline) {
				tb.Fatalf("server never registered conn %d", i)
			}
			time.Sleep(time.Millisecond)
		}
	}
	time.Sleep(300 * time.Millisecond) // let startup goroutines park
	idle = perConn(snapMem(), before, n)

	for i, c := range conns {
		if err := c.heartbeat(); err != nil {
			tb.Fatalf("heartbeat %d: %v", i, err)
		}
	}
	time.Sleep(300 * time.Millisecond)
	afterHB = perConn(snapMem(), before, n)

	for _, c := range conns {
		_ = c.Close()
	}
	conns = nil
	_ = srv.Shutdown(context.Background())
	return idle, afterHB
}

const footprintConns = 2000

// BenchmarkIdleFootprint reports the server-side cost of 2,000 idle connections
// and of the same connections after one Heartbeat each. Run with -benchtime=1x;
// each iteration builds and tears down a fresh server.
func BenchmarkIdleFootprint(b *testing.B) {
	var idle, hb footprint
	for i := 0; i < b.N; i++ {
		idle, hb = measureFootprint(b, footprintConns)
	}
	b.ReportMetric(idle.HeapPerConn, "idle-heap-B/conn")
	b.ReportMetric(idle.StackPerConn, "idle-stack-B/conn")
	b.ReportMetric(idle.GoroutinesPerConn, "idle-goroutines/conn")
	b.ReportMetric(hb.HeapPerConn, "hb-heap-B/conn")
	b.ReportMetric(hb.StackPerConn, "hb-stack-B/conn")
	b.ReportMetric(hb.GoroutinesPerConn, "hb-goroutines/conn")
}

// Upper bounds on the per-connection footprint. They sit a little above what
// the implementation measures today and are meant to be ratcheted down as the
// footprint shrinks; a failure means a per-connection cost was added.
const (
	maxIdleHeapPerConn   = 17 << 10 // measured ~14.5 KB; headroom for runtime and CI variance
	maxIdleStackPerConn  = 9 << 10 // measured ~4.3 KB (idle and after a Heartbeat); stack size classes jump by 4 KB // after-Heartbeat stack varies 20-25 KB with GC stack shrinking
	maxIdleGoroutinesPer = 1.5
)

// TestIdleFootprintBounds guards the idle footprint. The race detector inflates
// heap and stack several-fold, so under -race the numbers are only logged; CI
// runs this test once more without -race.
func TestIdleFootprintBounds(t *testing.T) {
	n := footprintConns
	if testing.Short() {
		n = 500
	}
	idle, hb := measureFootprint(t, n)
	t.Logf("n=%d idle: heap=%.0f B/conn stack=%.0f B/conn goroutines=%.2f/conn", n, idle.HeapPerConn, idle.StackPerConn, idle.GoroutinesPerConn)
	t.Logf("n=%d after heartbeat: heap=%.0f B/conn stack=%.0f B/conn goroutines=%.2f/conn", n, hb.HeapPerConn, hb.StackPerConn, hb.GoroutinesPerConn)
	if raceEnabled {
		t.Skip("bounds are not asserted under the race detector")
	}
	for name, f := range map[string]footprint{"idle": idle, "after-heartbeat": hb} {
		if f.HeapPerConn > maxIdleHeapPerConn {
			t.Errorf("%s heap %.0f B/conn exceeds %d", name, f.HeapPerConn, maxIdleHeapPerConn)
		}
		if f.StackPerConn > maxIdleStackPerConn {
			t.Errorf("%s stack %.0f B/conn exceeds %d", name, f.StackPerConn, maxIdleStackPerConn)
		}
		if f.GoroutinesPerConn > maxIdleGoroutinesPer {
			t.Errorf("%s goroutines %.2f/conn exceeds %.1f", name, f.GoroutinesPerConn, maxIdleGoroutinesPer)
		}
	}
}

func dialHeartbeatConn(ctx context.Context, tb testing.TB, ts *httptest.Server, id string) *websocket.Conn {
	tb.Helper()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/" + id
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	if err != nil {
		tb.Fatal(err)
	}
	return c
}

func heartbeatRoundTrip(ctx context.Context, c *websocket.Conn) error {
	if err := c.Write(ctx, websocket.MessageText, []byte(`[2,"1","Heartbeat",{}]`)); err != nil {
		return err
	}
	_, _, err := c.Read(ctx)
	return err
}

// BenchmarkHeartbeatMessage measures one inbound Heartbeat CALL -> CALLRESULT
// round trip on a single connection. Allocations include the coder/websocket
// client side of the loopback.
func BenchmarkHeartbeatMessage(b *testing.B) {
	_, ts := newHeartbeatServer(b)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c := dialHeartbeatConn(ctx, b, ts, "CP_BENCH")
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := heartbeatRoundTrip(ctx, c); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkHeartbeatMessageParallel runs Heartbeat round trips across 256
// connections, one connection per worker goroutine.
func BenchmarkHeartbeatMessageParallel(b *testing.B) {
	const conns = 256
	_, ts := newHeartbeatServer(b)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	cs := make([]*websocket.Conn, conns)
	for i := range cs {
		cs[i] = dialHeartbeatConn(ctx, b, ts, fmt.Sprintf("CP_%04d", i))
		defer func(c *websocket.Conn) { _ = c.Close(websocket.StatusNormalClosure, "") }(cs[i])
	}
	var next atomic.Int64
	b.SetParallelism(conns / runtime.GOMAXPROCS(0)) // at most one worker per conn
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		c := cs[int(next.Add(1)-1)%conns]
		for pb.Next() {
			if err := heartbeatRoundTrip(ctx, c); err != nil {
				b.Error(err)
				return
			}
		}
	})
}
