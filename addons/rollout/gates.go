package rollout

import (
	"fmt"
	"math"
	"sort"
	"time"
)

// Verdict is the result of one gate.
type Verdict string

// Verdicts.
const (
	Pass         Verdict = "pass"
	Fail         Verdict = "fail"
	Insufficient Verdict = "insufficient" // too few samples to say
)

// GateResult is one evaluated criterion.
type GateResult struct {
	Name    string  `json:"name"`
	Verdict Verdict `json:"verdict"`
	Detail  string  `json:"detail"`
}

// evaluate applies the promotion criteria to the canary and stable statistics.
func evaluate(g Gates, canary, stable PoolStats) []GateResult {
	g = g.withDefaults()
	var out []GateResult
	if canary.Requests < g.MinRequests {
		return []GateResult{{Name: "samples", Verdict: Insufficient,
			Detail: fmt.Sprintf("%.0f canary requests, %.0f needed", canary.Requests, g.MinRequests)}}
	}
	out = append(out, GateResult{Name: "samples", Verdict: Pass, Detail: fmt.Sprintf("%.0f canary requests", canary.Requests)})

	// Failed outcomes.
	cf, sf := ratio(canary.Failed, canary.Requests), ratio(stable.Failed, stable.Requests)
	r := GateResult{Name: "failed_ratio", Verdict: Pass, Detail: fmt.Sprintf("canary %.4f, stable %.4f, allowed +%.4f", cf, sf, g.MaxFailedDelta)}
	if cf > sf+g.MaxFailedDelta {
		r.Verdict = Fail
	}
	out = append(out, r)

	// CALLERROR code distribution.
	codes := map[string]bool{}
	for k := range canary.ByOutcome {
		codes[k] = true
	}
	var names []string
	for k := range codes {
		names = append(names, k)
	}
	sort.Strings(names)
	r = GateResult{Name: "callerror_codes", Verdict: Pass, Detail: "no code above its stable share"}
	for _, k := range names {
		cs, ss := ratio(canary.ByOutcome[k], canary.Requests), ratio(stable.ByOutcome[k], stable.Requests)
		if cs > ss+g.MaxCodeShareDelta {
			r = GateResult{Name: "callerror_codes", Verdict: Fail,
				Detail: fmt.Sprintf("%s: canary share %.4f, stable %.4f, allowed +%.4f", k, cs, ss, g.MaxCodeShareDelta)}
			break
		}
	}
	out = append(out, r)

	// Latency.
	switch {
	case !canary.HasLatency:
		out = append(out, GateResult{Name: "p99", Verdict: Pass, Detail: "no latency observations"})
	default:
		r = GateResult{Name: "p99", Verdict: Pass, Detail: fmt.Sprintf("canary %s", secs(canary.P99))}
		if stable.HasLatency && stable.P99 > 0 {
			r.Detail = fmt.Sprintf("canary %s, stable %s, allowed x%.2f", secs(canary.P99), secs(stable.P99), g.MaxP99Ratio)
			if canary.P99 > stable.P99*g.MaxP99Ratio {
				r.Verdict = Fail
			}
		}
		if g.MaxP99 > 0 && canary.P99 > time.Duration(g.MaxP99).Seconds() {
			r.Verdict = Fail
			r.Detail += fmt.Sprintf(" (budget %s)", time.Duration(g.MaxP99))
		}
		out = append(out, r)
	}
	return out
}

func ratio(n, d float64) float64 {
	if d <= 0 {
		return 0
	}
	return n / d
}

func secs(s float64) string {
	return time.Duration(math.Round(s*1e6) * float64(time.Microsecond)).String()
}

// worst returns the overall verdict of a result list: Fail beats Insufficient
// beats Pass.
func worst(rs []GateResult) Verdict {
	v := Pass
	for _, r := range rs {
		switch r.Verdict {
		case Fail:
			return Fail
		case Insufficient:
			v = Insufficient
		}
	}
	return v
}
