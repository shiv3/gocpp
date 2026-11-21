package benchmarks

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
)

// hopRig is a server with a real WebSocket in front of it, either embedded or
// split into a gateway and a processing tier over the in-process link.
type hopRig struct {
	ts   *httptest.Server
	conn *websocket.Conn
	stop func()
}

// newHopRig serves Heartbeat with schema validation off (the gateway's
// configuration, design 2.7) and connects one raw WebSocket client.
func newHopRig(tb testing.TB, split bool) *hopRig {
	tb.Helper()
	return newHopRigOpts(tb, split, nil)
}

// newHopRigOpts is newHopRig with extra options for the server that terminates
// the WebSocket (the gateway when split, the only server otherwise).
func newHopRigOpts(tb testing.TB, split bool, edgeOpts []csms.Option) *hopRig {
	tb.Helper()
	var handler func() *csms.Server
	var closers []func()
	if split {
		hub := inproc.New()
		gw := csms.NewGatewayServer(hub.Gateway(), append([]csms.Option{csms.WithSubProtocols("ocpp1.6"), csms.WithLogger(quiet)}, edgeOpts...)...)
		be := csms.NewServer(csms.WithGatewayLink(hub.Backend()), csms.WithSubProtocols("ocpp1.6"), csms.WithLogger(quiet))
		register(tb, be)
		drain := hub.AttachBackend(be.Upstream(), gwInfo())
		handler = func() *csms.Server { return gw }
		closers = append(closers, gw.Close, func() { _ = drain(context.Background()); be.Close() })
	} else {
		srv := csms.NewServer(append([]csms.Option{csms.WithSubProtocols("ocpp1.6"), csms.WithLogger(quiet)}, edgeOpts...)...)
		register(tb, srv)
		handler = func() *csms.Server { return srv }
		closers = append(closers, srv.Close)
	}
	ts := httptest.NewServer(handler().Handler())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[4:]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	if err != nil {
		tb.Fatal(err)
	}
	r := &hopRig{ts: ts, conn: c}
	r.stop = func() {
		_ = c.CloseNow()
		ts.Close()
		for _, f := range closers {
			f()
		}
	}
	return r
}

func register(tb testing.TB, s *csms.Server) {
	tb.Helper()
	if err := csms.On(s, profiles.Heartbeat, func(context.Context, *csms.Conn, messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{CurrentTime: time.Unix(0, 0).UTC()}, nil
	}); err != nil {
		tb.Fatal(err)
	}
}

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

var heartbeatFrame = []byte(`[2,"m1","Heartbeat",{}]`)

func (r *hopRig) roundTrip(ctx context.Context) error {
	if err := r.conn.Write(ctx, websocket.MessageText, heartbeatFrame); err != nil {
		return err
	}
	_, _, err := r.conn.Read(ctx)
	return err
}

// BenchmarkGatewayHop is one Heartbeat round trip over a real WebSocket, embedded
// versus through a gateway and a backend joined by the in-process link: the
// difference is the cost of the split (the gateway code and the hop) without
// network between the tiers. The gRPC link adds its own cost, measured in
// addons/link-grpc.
func BenchmarkGatewayHop(b *testing.B) {
	for _, split := range []bool{false, true} {
		name := "embedded"
		if split {
			name = "split-inproc"
		}
		b.Run(name, func(b *testing.B) {
			r := newHopRig(b, split)
			defer r.stop()
			ctx := context.Background()
			for i := 0; i < 200; i++ {
				if err := r.roundTrip(ctx); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := r.roundTrip(ctx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestGateHopOverhead is the in-library performance gate for the split: through
// the in-process link a message may cost at most 0.5 ms more on average and 2 ms
// more at p99 than the embedded server (design 2.7 allows 2 ms p99 for gateway
// plus network hop; the in-process hop has no network). The bounds are loose on
// purpose: they catch an accidental blocking call or a serialization point, not
// microsecond drift, and CI hosts are noisy. Real numbers are in docs/gateway.md.
func TestGateHopOverhead(t *testing.T) {
	if raceEnabled || testing.Short() {
		t.Skip("timing gate: not under -race or -short")
	}
	measure := func(split bool) (mean time.Duration, p99 time.Duration) {
		r := newHopRig(t, split)
		defer r.stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		for i := 0; i < 500; i++ {
			if err := r.roundTrip(ctx); err != nil {
				t.Fatal(err)
			}
		}
		const n = 5000
		lat := make([]time.Duration, 0, n)
		var sum time.Duration
		for i := 0; i < n; i++ {
			t0 := time.Now()
			if err := r.roundTrip(ctx); err != nil {
				t.Fatal(err)
			}
			d := time.Since(t0)
			lat = append(lat, d)
			sum += d
		}
		slices.Sort(lat)
		return sum / n, lat[n*99/100]
	}
	// Alternate and keep the best of three to ride out noise from other jobs.
	best := func(split bool) (mean, p99 time.Duration) {
		mean, p99 = time.Hour, time.Hour
		for i := 0; i < 3; i++ {
			m, p := measure(split)
			mean, p99 = min(mean, m), min(p99, p)
		}
		return
	}
	em, ep := best(false)
	sm, sp := best(true)
	t.Logf("embedded: mean %v p99 %v; split-inproc: mean %v p99 %v; added: mean %v p99 %v", em, ep, sm, sp, sm-em, sp-ep)
	if sm-em > 500*time.Microsecond {
		t.Errorf("split adds %v on average (gate 500us)", sm-em)
	}
	if sp-ep > 2*time.Millisecond {
		t.Errorf("split adds %v at p99 (gate 2ms)", sp-ep)
	}
}

func gwInfo() gateway.BackendInfo {
	return gateway.BackendInfo{Caps: []string{gateway.CapSend21}}
}
