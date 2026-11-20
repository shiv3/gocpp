package benchmarks

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/codec"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
)

// BenchmarkDispatchParallel drives many connections at once (one driver
// goroutine per connection, as many charge points would), with lenient schema
// validation and a typed Heartbeat handler, with and without a HandlerPool.
// ns/op is wall time per message across all connections.
const poolSize = 64

func BenchmarkDispatchParallel(b *testing.B) {
	reg := schema.NewRegistry()
	if err := v16.RegisterSchemas(reg); err != nil {
		b.Fatal(err)
	}
	for _, pooled := range []bool{false, true} {
		b.Run(fmt.Sprintf("pool=%v", pooled), func(b *testing.B) {
			cfg := dispatcher.DefaultConfig()
			cfg.PingInterval, cfg.ReadTimeout = 0, 0
			cfg.SchemaMode = dispatcher.SchemaModeLenient
			cfg.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
				v, ok := reg.Lookup(string(version), action, kind)
				if !ok {
					return payload, nil, nil
				}
				return v.ValidateLenientWith(payload, schema.LenientOptions{})
			}
			if pooled {
				cfg.HandlerPool = dispatcher.NewHandlerPool(poolSize)
				defer cfg.HandlerPool.Close()
			}
			hr := dispatcher.NewHandlerRegistry()
			hr.Register("Heartbeat", func(_ context.Context, _ *dispatcher.Conn, p []byte) ([]byte, error) {
				var req messages.HeartbeatRequest
				if err := codec.Unmarshal(p, &req); err != nil {
					return nil, err
				}
				return codec.Marshal(messages.HeartbeatResponse{CurrentTime: time.Unix(0, 0).UTC()})
			})
			const conns = 256
			frame := []byte(`[2,"bench-1","Heartbeat",{}]`)
			var wg sync.WaitGroup
			var left atomic.Int64
			left.Store(int64(b.N))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < conns; i++ {
				ws := transport.NewFakeWS("ocpp1.6")
				c := dispatcher.NewConn(fmt.Sprintf("CP_%d", i), ws, cfg, hr)
				c.Start(context.Background())
				wg.Add(1)
				go func() {
					defer wg.Done()
					defer func() { _ = c.Close(nil) }()
					sent := ws.Sent()
					for left.Add(-1) >= 0 {
						ws.Inject(frame)
						<-sent
					}
				}()
			}
			wg.Wait()
		})
	}
}
