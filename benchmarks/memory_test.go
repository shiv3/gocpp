package benchmarks

import (
	"context"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
)

// TestMemoryPerConnection measures the in-process heap per connection (both client
// and server sides), so the number is conservative versus a server-only deployment.
func TestMemoryPerConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping memory footprint test in -short mode")
	}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const conns = 200
	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	clients := make([]*cp.Client, 0, conns)
	for i := 0; i < conns; i++ {
		url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_" + itoa(i)
		c := cp.NewClient("CP_"+itoa(i), url, cp.WithSubProtocols("ocpp1.6"))
		if err := c.Connect(ctx); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()
	time.Sleep(time.Second)

	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	perConn := (after.HeapAlloc - before.HeapAlloc) / conns
	t.Logf("per-connection heap (client+server in-process): %d bytes", perConn)
	if perConn > 100*1024 {
		t.Errorf("per-connection heap %d exceeds 100KB", perConn)
	}
}
