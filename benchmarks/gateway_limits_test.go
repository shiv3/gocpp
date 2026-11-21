package benchmarks

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/shiv3/gocpp/csms"
)

// limitCase is one configuration of the connection-terminating server.
type limitCase struct {
	name  string
	split bool
	opts  []csms.Option
}

// neverRejects is a tenant budget no benchmark can exhaust, so the measurement
// is the cost of the check, not of rejecting.
var neverRejects = csms.WithTenantLimits(csms.TenantLimits{
	Default: csms.TenantLimit{MaxSessions: 1 << 20, MessageRate: 1e9, MessageBurst: 1 << 30},
})

func limitCases() []limitCase {
	return []limitCase{
		{"embedded", false, nil},
		{"embedded+tenant", false, []csms.Option{neverRejects}},
		{"split-inproc", true, nil},
		{"split-inproc+tenant", true, []csms.Option{neverRejects}},
		{"split-inproc+edge-heartbeat", true, []csms.Option{csms.WithEdgeHeartbeat()}},
		{"split-inproc+edge-heartbeat+tenant", true, []csms.Option{csms.WithEdgeHeartbeat(), neverRejects}},
	}
}

// BenchmarkEdgeAndTenant is one Heartbeat round trip over a real WebSocket for
// each combination of the opt-in gateway features: the edge Heartbeat responder
// (no backend hop) and the per-tenant limits (a session cap checked at upgrade
// and a token bucket on every inbound message).
func BenchmarkEdgeAndTenant(b *testing.B) {
	for _, c := range limitCases() {
		b.Run(c.name, func(b *testing.B) {
			r := newHopRigOpts(b, c.split, c.opts)
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

// TestGateEdgeAndTenantOverhead gates the two features: the tenant check may add
// at most 100 us on average (it is a mutex and a few float operations; the bound
// only catches an accidental blocking call), and answering Heartbeat at the edge
// must be cheaper than forwarding it through the in-process hop.
func TestGateEdgeAndTenantOverhead(t *testing.T) {
	if raceEnabled || testing.Short() {
		t.Skip("timing gate: not under -race or -short")
	}
	mean := func(c limitCase) time.Duration {
		best := time.Hour
		for i := 0; i < 3; i++ {
			r := newHopRigOpts(t, c.split, c.opts)
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			for j := 0; j < 500; j++ {
				if err := r.roundTrip(ctx); err != nil {
					t.Fatal(err)
				}
			}
			const n = 5000
			lat := make([]time.Duration, 0, n)
			for j := 0; j < n; j++ {
				t0 := time.Now()
				if err := r.roundTrip(ctx); err != nil {
					t.Fatal(err)
				}
				lat = append(lat, time.Since(t0))
			}
			cancel()
			r.stop()
			slices.Sort(lat)
			var sum time.Duration
			for _, d := range lat {
				sum += d
			}
			best = min(best, sum/n)
		}
		return best
	}
	cases := limitCases()
	byName := map[string]time.Duration{}
	for _, c := range cases {
		byName[c.name] = mean(c)
		t.Logf("%-40s mean %v", c.name, byName[c.name])
	}
	if d := byName["embedded+tenant"] - byName["embedded"]; d > 100*time.Microsecond {
		t.Errorf("tenant limits add %v on average (gate 100us)", d)
	}
	if byName["split-inproc+edge-heartbeat"] >= byName["split-inproc"] {
		t.Errorf("edge Heartbeat (%v) is not cheaper than the hop (%v)", byName["split-inproc+edge-heartbeat"], byName["split-inproc"])
	}
}
