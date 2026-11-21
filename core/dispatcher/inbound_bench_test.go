package dispatcher

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
)

// BenchmarkInboundHeartbeat measures one inbound CALL round trip through the
// handler path (parse, queue, handler, CALLRESULT write) on a fake transport.
func BenchmarkInboundHeartbeat(b *testing.B) {
	f := transport.NewFakeWS("ocpp1.6")
	reg := NewHandlerRegistry()
	reg.Register("Heartbeat", func(context.Context, *Conn, []byte) ([]byte, error) {
		return []byte(`{"currentTime":"2026-06-15T00:00:00Z"}`), nil
	})
	cfg := DefaultConfig()
	cfg.PingInterval = 0
	cfg.ReadTimeout = 0
	c := NewConn("CP", f, cfg, reg)
	c.Start(context.Background())
	defer func() { _ = c.Close(nil) }()
	msg := []byte(`[2,"m1","Heartbeat",{}]`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Inject(msg)
		<-f.Sent()
	}
}
