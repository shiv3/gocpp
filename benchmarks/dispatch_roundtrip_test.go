package benchmarks

import (
	"context"
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

// BenchmarkDispatchRoundTrip measures one inbound CALL -> typed handler ->
// CALLRESULT through dispatcher.Conn on an in-memory transport: no sockets and no
// client, so the numbers are the library's own per-message cost. The schema
// variants mirror how csms wires validation (see csms.NewServer).
func BenchmarkDispatchRoundTrip(b *testing.B) {
	reg := schema.NewRegistry()
	if err := v16.RegisterSchemas(reg); err != nil {
		b.Fatal(err)
	}
	cases := []struct {
		name, action string
		frame        string
		handler      dispatcher.HandlerFunc
	}{
		{"Heartbeat", "Heartbeat", `[2,"bench-1","Heartbeat",{}]`, func(_ context.Context, _ *dispatcher.Conn, p []byte) ([]byte, error) {
			var req messages.HeartbeatRequest
			if err := codec.Unmarshal(p, &req); err != nil {
				return nil, err
			}
			return codec.Marshal(messages.HeartbeatResponse{CurrentTime: time.Unix(0, 0).UTC()})
		}},
		{"StatusNotification", "StatusNotification",
			`[2,"bench-1","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Available","timestamp":"2026-11-21T01:02:03Z"}]`,
			func(_ context.Context, _ *dispatcher.Conn, p []byte) ([]byte, error) {
				var req messages.StatusNotificationRequest
				if err := codec.Unmarshal(p, &req); err != nil {
					return nil, err
				}
				return codec.Marshal(messages.StatusNotificationResponse{})
			}},
	}
	modes := []struct {
		name string
		mode dispatcher.SchemaMode
	}{{"schema=off", dispatcher.SchemaModeOff}, {"schema=lenient", dispatcher.SchemaModeLenient}, {"schema=strict", dispatcher.SchemaModeStrict}}

	for _, tc := range cases {
		for _, m := range modes {
			b.Run(tc.name+"/"+m.name, func(b *testing.B) {
				cfg := dispatcher.DefaultConfig()
				cfg.PingInterval = 0
				cfg.ReadTimeout = 0
				cfg.SchemaMode = m.mode
				switch m.mode {
				case dispatcher.SchemaModeLenient:
					cfg.SchemaValidateLenient = func(version ocppj.Version, action, kind string, payload []byte) ([]byte, []string, error) {
						v, ok := reg.Lookup(string(version), action, kind)
						if !ok {
							return payload, nil, nil
						}
						return v.ValidateLenientWith(payload, schema.LenientOptions{})
					}
				case dispatcher.SchemaModeStrict:
					cfg.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
						v, ok := reg.Lookup(string(version), action, kind)
						if !ok {
							return nil
						}
						return v.Validate(payload)
					}
				}
				hr := dispatcher.NewHandlerRegistry()
				hr.Register(tc.action, tc.handler)
				ws := transport.NewFakeWS("ocpp1.6")
				c := dispatcher.NewConn("CP_BENCH", ws, cfg, hr)
				c.Start(context.Background())
				defer func() { _ = c.Close(nil) }()

				frame := []byte(tc.frame)
				sent := ws.Sent()
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					ws.Inject(frame)
					<-sent
				}
			})
		}
	}
}
