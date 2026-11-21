package routepolicy_test

import (
	"fmt"
	"testing"

	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/routepolicy"
)

// BenchmarkRoute is the per-message cost of evaluating a routing policy on the
// gateway's hot path (docs/staged-releases.md, "Rollout under load"): no policy,
// the plain 10% percent rule, and a policy with an attribute rule in front of the
// percent rule, a `hold: busy` rule, and an action filter.
func BenchmarkRoute(b *testing.B) {
	ids := make([]string, 4096)
	for i := range ids {
		ids[i] = fmt.Sprintf("LT-%05d", i)
	}
	attrs := map[string]string{"site": "s1", "vendor": "acme", "firmware": "2.1.4", "label.canary": "0"}
	type tc struct {
		name   string
		policy *routepolicy.Policy
	}
	pc := 10.0
	cases := []tc{
		{"no_policy", nil},
		{"percent", &routepolicy.Policy{Revision: 1, Salt: "s", Rules: []routepolicy.Rule{
			{Name: "canary", Pool: "canary", Match: routepolicy.Match{Percent: &pc}}}}},
		{"attrs_then_percent", &routepolicy.Policy{Revision: 1, Salt: "s", Rules: []routepolicy.Rule{
			{Name: "label", Pool: "canary", Match: routepolicy.Match{Attrs: map[string][]string{"label.canary": {"1"}}}},
			{Name: "fw", Pool: "canary", Match: routepolicy.Match{Attrs: map[string][]string{"vendor": {"acme"}, "firmware": {"2.2.*"}}, Percent: &pc}},
			{Name: "canary", Pool: "canary", Match: routepolicy.Match{Percent: &pc}}}}},
		{"hold_busy", &routepolicy.Policy{Revision: 1, Salt: "s", Rules: []routepolicy.Rule{
			{Name: "canary", Pool: "canary", Hold: "busy", Match: routepolicy.Match{Percent: &pc}}}}},
	}
	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			e := routepolicy.NewEngine()
			if c.policy != nil {
				if err := e.Apply(*c.policy); err != nil {
					b.Fatal(err)
				}
			}
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				in := gateway.RouteInput{Action: "Heartbeat", Kind: gateway.KindCall, Attrs: attrs}
				for pb.Next() {
					in.CPID = ids[i&4095]
					i++
					_ = e.Route(in)
				}
			})
		})
	}
}
