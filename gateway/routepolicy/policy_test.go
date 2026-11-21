package routepolicy_test

import (
	"fmt"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
	"pgregory.net/rapid"
)

const designYAML = `
revision: 42
default: default
salt: rollout-0-9-3
rules:
  - name: lab
    pool: canary
    match:
      attrs: { label.canary: ["1"] }
  - name: step
    pool: canary
    match:
      attrs: { tenant: ["t1"], vendor: ["Acme"], firmware: ["2.1.*"] }
      ocpp: ["ocpp1.6"]
      percent: 5
    on_unavailable: fallback
    hold: busy
`

func TestParseYAMLAndJSON(t *testing.T) {
	p, err := routepolicy.Parse([]byte(designYAML))
	require.NoError(t, err)
	require.NoError(t, p.Validate())
	require.EqualValues(t, 42, p.Revision)
	require.Len(t, p.Rules, 2)
	require.Equal(t, []string{"2.1.*"}, p.Rules[1].Match.Attrs["firmware"])
	require.Equal(t, 5.0, *p.Rules[1].Match.Percent)

	b, err := routepolicy.Marshal(p)
	require.NoError(t, err)
	q, err := routepolicy.Parse(b)
	require.NoError(t, err)
	require.Equal(t, p, q)

	_, err = routepolicy.Parse([]byte("revision: 1\nbogus: true\n"))
	require.Error(t, err, "unknown fields are refused")
	d, err := routepolicy.Parse([]byte(`{"revision":2,"rules":[{"name":"a","pool":"c","suspend_for":"30s","guard":{"min_requests":10,"max_error_ratio":0.1,"window":"1m"}}]}`))
	require.NoError(t, err)
	require.NoError(t, d.Validate())
}

func TestValidate(t *testing.T) {
	bad := map[string]string{
		"no name":        `{"rules":[{"pool":"c"}]}`,
		"no pool":        `{"rules":[{"name":"a"}]}`,
		"dup":            `{"rules":[{"name":"a","pool":"c"},{"name":"a","pool":"c"}]}`,
		"percent":        `{"rules":[{"name":"a","pool":"c","match":{"percent":101}}]}`,
		"on_unavailable": `{"rules":[{"name":"a","pool":"c","on_unavailable":"x"}]}`,
		"hold":           `{"rules":[{"name":"a","pool":"c","hold":"x"}]}`,
		"guard":          `{"rules":[{"name":"a","pool":"c","guard":{"min_requests":0}}]}`,
		"version":        `{"rules":[{"name":"a","pool":"c","match":{"ocpp":["3.0"]}}]}`,
		"split 1.6":      `{"rules":[{"name":"a","pool":"c","match":{"ocpp":["1.6"]},"actions":["StartTransaction","Authorize"]}]}`,
		"split shared":   `{"rules":[{"name":"a","pool":"c","actions":["MeterValues"]}]}`,
		"split 2.x":      `{"rules":[{"name":"a","pool":"c","actions":["TransactionEvent"]}]}`,
	}
	for name, doc := range bad {
		p, err := routepolicy.Parse([]byte(doc))
		require.NoError(t, err, name)
		require.Error(t, p.Validate(), name)
	}
	good := map[string]string{
		"disjoint action": `{"rules":[{"name":"a","pool":"c","actions":["DataTransfer","FirmwareStatusNotification"]}]}`,
		"whole 1.6 group": `{"rules":[{"name":"a","pool":"c","match":{"ocpp":["1.6"]},"actions":["Authorize","StartTransaction","StopTransaction","MeterValues","StatusNotification"]}]}`,
		"whole 2.x group": `{"rules":[{"name":"a","pool":"c","match":{"ocpp":["2.0.1"]},"actions":["Authorize","TransactionEvent","MeterValues","StatusNotification"]}]}`,
		"allow_split":     `{"rules":[{"name":"a","pool":"c","allow_split":true,"actions":["MeterValues"]}]}`,
		"boot only":       `{"rules":[{"name":"a","pool":"c","actions":["BootNotification"]}]}`,
	}
	for name, doc := range good {
		p, err := routepolicy.Parse([]byte(doc))
		require.NoError(t, err, name)
		require.NoError(t, p.Validate(), name)
	}
}

func eng(t testing.TB, doc string) *routepolicy.Engine {
	t.Helper()
	e := routepolicy.NewEngine()
	require.NoError(t, e.ApplyData(0, []byte(doc)))
	return e
}

func TestRouteMatching(t *testing.T) {
	e := eng(t, designYAML)
	in := gateway.RouteInput{CPID: "cp1", Action: "Heartbeat", OCPP: "1.6", Attrs: map[string]string{"label.canary": "1"}}
	d := e.Route(in)
	require.Equal(t, gateway.Route{Pool: "canary", Fallback: "default", Rule: "lab", Rev: 42}, d)

	// attribute rule: AND across keys, prefix match, version.
	in.Attrs = map[string]string{"tenant": "t1", "vendor": "Acme", "firmware": "2.1.7"}
	hit := 0
	for i := 0; i < 2000; i++ {
		in.CPID = fmt.Sprintf("cp%d", i)
		if e.Route(in).Rule == "step" {
			hit++
		}
	}
	require.InDelta(t, 100, hit, 40, "5% of 2000")
	in.Attrs["firmware"] = "2.2.0"
	require.Equal(t, "", e.Route(in).Rule)
	in.Attrs["firmware"] = "2.1.7"
	in.OCPP = "2.0.1"
	require.Equal(t, "", e.Route(in).Rule)

	// $connect has no attributes: attribute rules do not match.
	require.Equal(t, gateway.Route{Pool: "default", Rev: 42}, e.Route(gateway.RouteInput{CPID: "cp1", Action: "$connect"}))
}

func TestRouteWithoutPolicyIsDefaultPool(t *testing.T) {
	e := routepolicy.NewEngine()
	require.Equal(t, gateway.Route{Pool: gateway.DefaultPool}, e.Route(gateway.RouteInput{CPID: "x"}))
}

func TestApplyRejectsInvalidAndStale(t *testing.T) {
	e := eng(t, `{"revision":5,"default":"stable"}`)
	require.ErrorIs(t, e.ApplyData(0, []byte(`{"revision":5}`)), routepolicy.ErrStaleRevision)
	require.ErrorIs(t, e.ApplyData(0, []byte(`{"revision":4}`)), routepolicy.ErrStaleRevision)
	require.Error(t, e.ApplyData(0, []byte(`{"revision":6,"rules":[{"name":"x"}]}`)))
	require.Error(t, e.ApplyData(0, []byte(`not: [valid`)))
	require.EqualValues(t, 5, e.Revision())
	require.Equal(t, "stable", e.Route(gateway.RouteInput{CPID: "a"}).Pool)
	// the store's revision overrides the document's
	require.NoError(t, e.ApplyData(9, []byte(`{"revision":1,"default":"x"}`)))
	require.EqualValues(t, 9, e.Revision())
}

func TestBucketDistribution(t *testing.T) {
	const n = 100000
	for _, pc := range []float64{1, 5, 10, 50} {
		in := 0
		for i := 0; i < n; i++ {
			if float64(routepolicy.Bucket("salt-a", fmt.Sprintf("CP-%06d", i))) < pc*100 {
				in++
			}
		}
		got := float64(in) / n * 100
		require.InDelta(t, pc, got, 1.0, "percent %v", pc)
	}
}

// Raising the percentage only adds chargers; the same salt keeps the sample.
func TestPropertyPercentMonotonic(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		salt := rapid.String().Draw(t, "salt")
		p1 := rapid.Float64Range(0, 100).Draw(t, "p1")
		p2 := rapid.Float64Range(p1, 100).Draw(t, "p2")
		cp := rapid.StringMatching(`[A-Za-z0-9_-]{1,24}`).Draw(t, "cp")
		mk := func(p float64) *routepolicy.Engine {
			e := routepolicy.NewEngine()
			require.NoError(t, e.Apply(routepolicy.Policy{Revision: 1, Salt: salt, Rules: []routepolicy.Rule{
				{Name: "r", Pool: "canary", Match: routepolicy.Match{Percent: &p}}}}))
			return e
		}
		in := gateway.RouteInput{CPID: cp, Action: "Heartbeat"}
		low, high := mk(p1).Route(in).Rule == "r", mk(p2).Route(in).Rule == "r"
		if low && !high {
			t.Fatalf("charger %q in at %v%% but out at %v%%", cp, p1, p2)
		}
	})
}

// Two gateways on the same revision route every message identically, and a
// charger's pool does not depend on message order.
func TestPropertyDeterminismAcrossGateways(t *testing.T) {
	a, b := eng(t, designYAML), eng(t, designYAML)
	rapid.Check(t, func(t *rapid.T) {
		in := gateway.RouteInput{
			CPID:   rapid.StringMatching(`CP-[0-9]{1,5}`).Draw(t, "cp"),
			Action: rapid.SampledFrom([]string{"Heartbeat", "BootNotification", "$connect", "$opened"}).Draw(t, "action"),
			OCPP:   ocppj.Version(rapid.SampledFrom([]string{"1.6", "2.0.1", ""}).Draw(t, "ver")),
			Attrs: rapid.MapOf(rapid.SampledFrom([]string{"tenant", "vendor", "firmware", "label.canary"}),
				rapid.SampledFrom([]string{"t1", "Acme", "2.1.3", "1", "x"})).Draw(t, "attrs"),
		}
		in2 := in
		in2.OCPP = in.OCPP
		require.Equal(t, a.Route(in), b.Route(in2))
		require.Equal(t, a.Route(in), a.Route(in), "sticky: same input, same decision")
	})
}
