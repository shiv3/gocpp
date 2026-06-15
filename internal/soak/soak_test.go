//go:build soak

package soak

import (
	"context"
	"net/http/httptest"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

type hbReq struct{}
type hbResp struct {
	CurrentTime string `json:"currentTime"`
}

var hbMsg = ocppj.Message[hbReq, hbResp]{Action: "Heartbeat", Direction: ocppj.SentByCP}

// TestSoak_LongRun connects many charge points, drives heartbeats for SOAK_DURATION
// (default 30s; CI sets minutes), and asserts goroutine/heap stay bounded.
func TestSoak_LongRun(t *testing.T) {
	dur := 30 * time.Second
	if v := os.Getenv("SOAK_DURATION"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			dur = d
		}
	}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	_ = csms.On(srv, hbMsg, func(ctx context.Context, c *csms.Conn, req hbReq) (hbResp, error) {
		return hbResp{CurrentTime: time.Now().Format(time.RFC3339)}, nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	const conns = 500
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	clients := make([]*cp.Client, 0, conns)
	for i := 0; i < conns; i++ {
		url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_" + itoa(i)
		c := cp.NewClient("CP_"+itoa(i), url, cp.WithSubProtocols("ocpp1.6"))
		require.NoError(t, c.Connect(ctx))
		clients = append(clients, c)
	}
	defer func() {
		for _, c := range clients {
			c.Close()
		}
	}()

	runtime.GC()
	startGoroutines := runtime.NumGoroutine()
	var startMem runtime.MemStats
	runtime.ReadMemStats(&startMem)

	deadline := time.After(dur)
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
loop:
	for {
		select {
		case <-deadline:
			break loop
		case <-ticker.C:
			for _, c := range clients {
				go func(c *cp.Client) { _, _ = cp.Call(ctx, c, hbMsg, hbReq{}) }(c)
			}
		}
	}

	time.Sleep(500 * time.Millisecond)
	runtime.GC()
	endGoroutines := runtime.NumGoroutine()
	var endMem runtime.MemStats
	runtime.ReadMemStats(&endMem)

	require.LessOrEqualf(t, endGoroutines-startGoroutines, 100, "goroutine growth %d->%d", startGoroutines, endGoroutines)
	heapGrowth := float64(endMem.HeapAlloc) / float64(startMem.HeapAlloc)
	require.Lessf(t, heapGrowth, 1.50, "heap growth ratio %.2f", heapGrowth)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b [20]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(b[pos:])
}
