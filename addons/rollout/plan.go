package rollout

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shiv3/gocpp/gateway/routepolicy"
	"gopkg.in/yaml.v3"
)

// Plan describes one automated rollout.
type Plan struct {
	// Base is the routing policy the rollout starts from. It must contain the rule
	// named Rule; the controller sets that rule's percent from the current stage and
	// leaves everything else as written. Its Revision is ignored.
	Base routepolicy.Policy `json:"base"`
	// Rule is the name of the rule the controller steers.
	Rule string `json:"rule"`
	// CanaryPool and StablePool are the pools compared (defaults: the rule's pool
	// and the policy's default pool).
	CanaryPool string `json:"canary_pool,omitempty"`
	StablePool string `json:"stable_pool,omitempty"`
	// Stages are applied in order; the rule's percent is set to Stage.Percent.
	Stages []Stage `json:"stages"`
	// Interval is the evaluation period (default 1m).
	Interval routepolicy.Duration `json:"interval,omitempty"`
	// Window is the metric window of an evaluation (default 5m); a stage is
	// evaluated over min(Window, time since the gateways converged on it).
	Window routepolicy.Duration `json:"window,omitempty"`
	// ConvergeTimeout bounds how long a revision may take to reach every gateway
	// before the controller keeps waiting and reports it (default 30s).
	ConvergeTimeout routepolicy.Duration `json:"converge_timeout,omitempty"`
	// FailEvaluations is how many consecutive failing evaluations roll back
	// (default 1).
	FailEvaluations int   `json:"fail_evaluations,omitempty"`
	Gates           Gates `json:"gates,omitempty"`
}

// Stage is one step of a rollout.
type Stage struct {
	Name    string               `json:"name,omitempty"`
	Percent float64              `json:"percent"`
	Soak    routepolicy.Duration `json:"soak"`
}

// Gates are the promotion criteria (docs/design/gateway.md 5.4). Zero values
// take the defaults.
type Gates struct {
	// MinRequests is the number of canary requests an evaluation needs; with fewer
	// the controller makes no decision (default 500).
	MinRequests float64 `json:"min_requests,omitempty"`
	// MaxFailedDelta: canary failed-outcome ratio (timeout, stream_broken,
	// callerror_InternalError) may exceed stable's by at most this (default 0.005).
	MaxFailedDelta float64 `json:"max_failed_delta,omitempty"`
	// MaxCodeShareDelta: no CALLERROR code may have a share of the canary's
	// requests more than this above its share of stable's (default 0.01).
	MaxCodeShareDelta float64 `json:"max_code_share_delta,omitempty"`
	// MaxP99Ratio: canary p99 upstream latency at most this multiple of stable's
	// (default 1.5).
	MaxP99Ratio float64 `json:"max_p99_ratio,omitempty"`
	// MaxP99 is an absolute p99 budget (0 = none).
	MaxP99 routepolicy.Duration `json:"max_p99,omitempty"`
	// LateOrphan turns on the late_replies / orphan_results gates: the canary's
	// count per request may exceed stable's by at most MaxLateOrphanDelta
	// (design 5.4: "not above stable"). Needs gateways that export
	// gocpp_gateway_late_replies_total and gocpp_gateway_orphan_results_total.
	LateOrphan bool `json:"late_orphan,omitempty"`
	// MaxLateOrphanDelta is the allowed excess of the per-request ratio of each
	// of the two counters over stable's (default 0.001).
	MaxLateOrphanDelta float64 `json:"max_late_orphan_delta,omitempty"`
	// Checks are application-level queries (PromSource only): each must evaluate
	// to at most Max.
	Checks []Check `json:"checks,omitempty"`
}

// Check is an extra gate: a Prometheus instant query that must stay at or below
// Max. {{.Window}} (e.g. "5m"), {{.Canary}} and {{.Stable}} are substituted.
type Check struct {
	Name  string  `json:"name"`
	Query string  `json:"query"`
	Max   float64 `json:"max"`
}

func (g Gates) withDefaults() Gates {
	if g.MinRequests <= 0 {
		g.MinRequests = 500
	}
	if g.MaxFailedDelta <= 0 {
		g.MaxFailedDelta = 0.005
	}
	if g.MaxCodeShareDelta <= 0 {
		g.MaxCodeShareDelta = 0.01
	}
	if g.MaxLateOrphanDelta <= 0 {
		g.MaxLateOrphanDelta = 0.001
	}
	if g.MaxP99Ratio <= 0 {
		g.MaxP99Ratio = 1.5
	}
	return g
}

// Parse reads a plan (YAML or JSON; unknown fields are an error).
func Parse(data []byte) (Plan, error) {
	var p Plan
	if !json.Valid(data) {
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			return p, fmt.Errorf("rollout: %w", err)
		}
		j, err := json.Marshal(v)
		if err != nil {
			return p, fmt.Errorf("rollout: %w", err)
		}
		data = j
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("rollout: %w", err)
	}
	return p, p.Validate()
}

// Validate checks the plan, including the base policy.
func (p Plan) Validate() error {
	var errs []error
	if err := p.Base.Validate(); err != nil {
		errs = append(errs, err)
	}
	if _, ok := p.rule(); !ok {
		errs = append(errs, fmt.Errorf("rule %q is not in the base policy", p.Rule))
	}
	if len(p.Stages) == 0 {
		errs = append(errs, errors.New("at least one stage is required"))
	}
	prev := -1.0
	for i, s := range p.Stages {
		if s.Percent <= 0 || s.Percent > 100 {
			errs = append(errs, fmt.Errorf("stage %d: percent %v outside (0, 100]", i, s.Percent))
		}
		if s.Percent < prev {
			errs = append(errs, fmt.Errorf("stage %d: percent %v is lower than the stage before (stages only grow)", i, s.Percent))
		}
		if s.Soak < 0 {
			errs = append(errs, fmt.Errorf("stage %d: negative soak", i))
		}
		prev = s.Percent
	}
	return errors.Join(errs...)
}

func (p Plan) rule() (routepolicy.Rule, bool) {
	for _, r := range p.Base.Rules {
		if r.Name == p.Rule {
			return r, true
		}
	}
	return routepolicy.Rule{}, false
}

func (p Plan) canary() string {
	if p.CanaryPool != "" {
		return p.CanaryPool
	}
	r, _ := p.rule()
	return r.Pool
}

func (p Plan) stable() string {
	if p.StablePool != "" {
		return p.StablePool
	}
	return p.Base.DefaultPool()
}

func (p Plan) interval() time.Duration {
	if p.Interval > 0 {
		return time.Duration(p.Interval)
	}
	return time.Minute
}

func (p Plan) window() time.Duration {
	if p.Window > 0 {
		return time.Duration(p.Window)
	}
	return 5 * time.Minute
}

func (p Plan) convergeTimeout() time.Duration {
	if p.ConvergeTimeout > 0 {
		return time.Duration(p.ConvergeTimeout)
	}
	return 30 * time.Second
}

// policyFor is the base policy with the managed rule at the stage's percent.
func (p Plan) policyFor(s Stage) routepolicy.Policy {
	pol := p.Base
	pol.Rules = append([]routepolicy.Rule(nil), p.Base.Rules...)
	for i := range pol.Rules {
		if pol.Rules[i].Name == p.Rule {
			pc := s.Percent
			pol.Rules[i].Match.Percent = &pc
		}
	}
	return pol
}
