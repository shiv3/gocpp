package dispatcher

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
)

// autoWS is an in-process transport that answers every CALL it is sent with an
// empty CALLRESULT, so benchmarks measure the library, not a network.
type autoWS struct {
	in   chan []byte
	done chan struct{}
	once sync.Once
}

func newAutoWS() *autoWS {
	return &autoWS{in: make(chan []byte, 64), done: make(chan struct{})}
}

func (a *autoWS) Read(ctx context.Context) ([]byte, error) {
	select {
	case m := <-a.in:
		return m, nil
	case <-a.done:
		return nil, transport.ErrFakeClosed
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (a *autoWS) Write(ctx context.Context, data []byte) error {
	f, err := ocppj.Parse(data)
	if err != nil || f.Type != ocppj.Call {
		return nil
	}
	select {
	case a.in <- []byte(`[3,"` + f.MsgID + `",{}]`):
	case <-a.done:
	}
	return nil
}

func (a *autoWS) Ping(context.Context) error { return nil }
func (a *autoWS) Close(transport.StatusCode, string) error {
	a.once.Do(func() { close(a.done) })
	return nil
}
func (a *autoWS) Subprotocol() string { return "ocpp1.6" }

func benchConn(serialize bool) *Conn {
	cfg := DefaultConfig()
	cfg.SerializeOutboundCalls = serialize
	cfg.PingInterval = 0
	cfg.ReadTimeout = 0
	c := NewConn("CP", newAutoWS(), cfg, NewHandlerRegistry())
	c.Start(context.Background())
	return c
}

var modes = []struct {
	name      string
	serialize bool
}{{"serialized", true}, {"concurrent", false}}

// (a) latency overhead of the queue: one connection, sequential calls.
func BenchmarkCallSequential(b *testing.B) {
	for _, m := range modes {
		b.Run(m.name, func(b *testing.B) {
			c := benchConn(m.serialize)
			defer func() { _ = c.Close(nil) }()
			ctx := context.Background()
			payload := []byte(`{}`)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := DoCall(ctx, c, "Heartbeat", payload); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// (b) many independent connections each calling in parallel: aggregate
// throughput and allocations per call.
func BenchmarkCallManyConns(b *testing.B) {
	for _, n := range []int{1000, 10000} {
		for _, m := range modes {
			b.Run(fmt.Sprintf("conns=%d/%s", n, m.name), func(b *testing.B) {
				conns := make([]*Conn, n)
				for i := range conns {
					conns[i] = benchConn(m.serialize)
				}
				defer func() {
					var wg sync.WaitGroup
					for _, c := range conns {
						wg.Add(1)
						go func() { defer wg.Done(); _ = c.Close(nil) }()
					}
					wg.Wait()
				}()
				ctx := context.Background()
				payload := []byte(`{}`)
				var ms0, ms1 runtime.MemStats
				runtime.GC()
				runtime.ReadMemStats(&ms0)
				b.ResetTimer()
				for r := 0; r < b.N; r++ {
					var wg sync.WaitGroup
					wg.Add(n)
					for _, c := range conns {
						go func() {
							defer wg.Done()
							if _, err := DoCall(ctx, c, "Heartbeat", payload); err != nil {
								b.Error(err)
							}
						}()
					}
					wg.Wait()
				}
				b.StopTimer()
				runtime.ReadMemStats(&ms1)
				calls := float64(n) * float64(b.N)
				b.ReportMetric(calls/b.Elapsed().Seconds(), "calls/s")
				b.ReportMetric(float64(ms1.Mallocs-ms0.Mallocs)/calls, "allocs/call")
			})
		}
	}
}
