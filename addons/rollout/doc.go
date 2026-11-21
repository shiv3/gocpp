// Package rollout is the optional automatic promotion controller of a backend
// canary (docs/design/gateway.md 5.4, phase 7c). It steers one rule of the
// routing policy through a list of stages (for example 1% → 10% → 50% → 100%),
// waits for every gateway to run each revision, evaluates the promotion criteria
// against canary and stable metrics, advances after the soak time, and rolls back
// with one revision when a gate fails or the gateway-local guard has suspended the
// rule. It writes through routepolicy.Controller, the same compare-and-set path
// as gocppctl route, and yields (stops) when somebody else changes the policy.
//
// It is off by default: nothing runs unless you start a Controller, and a
// Controller with DryRun set evaluates and reports what it would do without
// writing anything. The last stage leaves the rule at its final percentage and
// reports "done": promoting the stable Deployment stays a deliberate step (see
// docs/rollout.md).
//
// Metrics come from a Source: a Prometheus server (PromSource) or the gateways'
// own /metrics endpoints (ScrapeSource), so the controller works with or without
// a Prometheus installation.
//
// Experimental: this API may change before v1.0. Every exported identifier of
// this package is part of the split-gateway API; see "Stability" in
// docs/gateway.md.
package rollout
