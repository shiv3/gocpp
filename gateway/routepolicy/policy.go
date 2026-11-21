// Package routepolicy is the built-in routing policy of a gateway
// (docs/design/gateway.md 5.1.2): a declarative, first-match rule list that
// decides which backend pool serves each message. It implements gateway.Router
// (Engine) and gateway.RouteObserver, loads policy documents from a file or a
// storage.PolicyStore, and guards a canary pool (suspend on fallback, error-rate
// guard, hold while a transaction runs).
//
// A decision is a pure function of (policy revision, charge point id, session
// attributes, OCPP version, action) plus two pieces of deliberately bounded state:
// rules suspended by the guard and the hold of busy sessions. Two gateways on the
// same revision therefore route a charger identically.
//
// Experimental: this API may change before v1.0. Every exported identifier of
// this package is part of the split-gateway API; see "Stability" in
// docs/gateway.md.
package routepolicy

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/shiv3/gocpp/gateway"
	"gopkg.in/yaml.v3"
)

// Policy is one revision of the routing policy.
type Policy struct {
	// Revision is strictly increasing; a gateway ignores revisions that are not
	// greater than the one it runs. When the policy is read from a
	// storage.PolicyStore the store's revision overrides this field.
	Revision uint64 `json:"revision"`
	// Default is the pool for everything no rule matches ("" = gateway.DefaultPool).
	Default string `json:"default,omitempty"`
	// Salt seeds the percentage hash. Keep it for all steps of one rollout (the
	// sample then only grows); change it for the next rollout.
	Salt string `json:"salt,omitempty"`
	// IgnoreHold makes this revision ignore hold: busy (written by a rollback).
	IgnoreHold bool `json:"ignore_hold,omitempty"`
	// Rules are evaluated in order; the first match wins.
	Rules []Rule `json:"rules,omitempty"`
}

// Rule routes the messages it matches to Pool.
type Rule struct {
	Name  string `json:"name"`
	Pool  string `json:"pool"`
	Match Match  `json:"match,omitempty"`
	// Actions restricts the rule to these OCPP actions; empty = all.
	Actions []string `json:"actions,omitempty"`
	// AllowSplit permits an Actions set that separates the members of a
	// transaction group (see Validate).
	AllowSplit bool `json:"allow_split,omitempty"`
	// OnUnavailable is "fallback" (default: use the policy's default pool when
	// Pool has no member) or "error" (hold, then CALLERROR).
	OnUnavailable string `json:"on_unavailable,omitempty"`
	// Hold is "none" (default) or "busy": a session whose gocpp.busy attribute is
	// set keeps its previous membership of this rule until it is cleared, the
	// session ends, or MaxHold passes.
	Hold    string   `json:"hold,omitempty"`
	MaxHold Duration `json:"max_hold,omitempty"`
	// SuspendFor is how long the rule stays suspended after a fallback (default 60 s).
	SuspendFor Duration `json:"suspend_for,omitempty"`
	// Guard suspends the rule until the next revision when the error ratio of its
	// pool is too high.
	Guard *Guard `json:"guard,omitempty"`
}

// Match selects sessions. All conditions are AND-ed; an empty Match matches all.
type Match struct {
	// Attrs: AND across keys, OR across values; a value ending in "*" is a
	// prefix match.
	Attrs map[string][]string `json:"attrs,omitempty"`
	// OCPP lists protocol versions ("1.6", "2.0.1", "2.1"; an "ocpp" prefix is accepted).
	OCPP []string `json:"ocpp,omitempty"`
	// CPIDs, when non-empty, restricts the rule to these charge point ids.
	CPIDs []string `json:"cp_ids,omitempty"`
	// Percent of the chargers passing the other conditions, 0..100 (nil = 100).
	Percent *float64 `json:"percent,omitempty"`
}

// Guard is the gateway-local automatic rollback of a rule.
type Guard struct {
	MinRequests   int      `json:"min_requests"`
	MaxErrorRatio float64  `json:"max_error_ratio"`
	Window        Duration `json:"window"`
}

// Duration is a time.Duration that reads and writes "60s" style strings.
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// UnmarshalJSON implements json.Unmarshaler; a number is read as seconds.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		v, err := time.ParseDuration(s)
		if err != nil {
			return err
		}
		*d = Duration(v)
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err != nil {
		return fmt.Errorf("duration: %s", b)
	}
	*d = Duration(f * float64(time.Second))
	return nil
}

// Defaults applied by the engine.
const (
	DefaultSuspendFor = 60 * time.Second
	DefaultMaxHold    = 4 * time.Hour
	// BusyAttr is the session attribute a backend sets while a transaction runs.
	BusyAttr = "gocpp.busy"
)

// Parse reads a policy document, JSON or YAML (JSON is a subset of YAML, so the
// format is detected by trying JSON first). Unknown fields are an error.
func Parse(data []byte) (Policy, error) {
	var p Policy
	if !json.Valid(data) {
		var v any
		if err := yaml.Unmarshal(data, &v); err != nil {
			return p, fmt.Errorf("routepolicy: %w", err)
		}
		j, err := json.Marshal(v)
		if err != nil {
			return p, fmt.Errorf("routepolicy: %w", err)
		}
		data = j
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return Policy{}, fmt.Errorf("routepolicy: %w", err)
	}
	return p, nil
}

// Marshal encodes p as indented JSON, the canonical stored form.
func Marshal(p Policy) ([]byte, error) { return json.MarshalIndent(p, "", "  ") }

// DefaultPool returns the pool of everything no rule matches.
func (p Policy) DefaultPool() string {
	if p.Default == "" {
		return gateway.DefaultPool
	}
	return p.Default
}

// Pools returns the pools the policy names, sorted.
func (p Policy) Pools() []string {
	set := map[string]bool{p.DefaultPool(): true}
	for _, r := range p.Rules {
		set[r.Pool] = true
	}
	out := make([]string, 0, len(set))
	for n := range set {
		out = append(out, n)
	}
	slices.Sort(out)
	return out
}

// Transaction groups (docs/design/gateway.md 5.1.2): the actions that must not
// be routed to different pools unless the rule says allow_split.
var (
	groupV16 = []string{"Authorize", "StartTransaction", "StopTransaction", "MeterValues", "StatusNotification"}
	groupV2x = []string{"Authorize", "TransactionEvent", "MeterValues", "StatusNotification"}
)

func normVersion(v string) string { return strings.TrimPrefix(strings.TrimSpace(v), "ocpp") }

func isV16(v string) bool { return v == "1.6" }
func isV2x(v string) bool { return strings.HasPrefix(v, "2.") }

// Validate checks p. It rejects: an empty rule name or pool, duplicate names,
// percent outside 0..100, unknown on_unavailable / hold values, a malformed
// guard or attribute matcher, an unknown OCPP version, and an action set that
// separates the members of a transaction group without allow_split.
func (p Policy) Validate() error {
	var errs []error
	seen := map[string]bool{}
	for i, r := range p.Rules {
		at := fmt.Sprintf("rule %d (%q)", i, r.Name)
		if r.Name == "" {
			errs = append(errs, fmt.Errorf("rule %d: name is required", i))
		} else if seen[r.Name] {
			errs = append(errs, fmt.Errorf("%s: duplicate name", at))
		}
		seen[r.Name] = true
		if r.Pool == "" {
			errs = append(errs, fmt.Errorf("%s: pool is required", at))
		}
		if pc := r.Match.Percent; pc != nil && (*pc < 0 || *pc > 100) {
			errs = append(errs, fmt.Errorf("%s: percent %v outside 0..100", at, *pc))
		}
		switch r.OnUnavailable {
		case "", "fallback", "error":
		default:
			errs = append(errs, fmt.Errorf("%s: on_unavailable %q (want fallback or error)", at, r.OnUnavailable))
		}
		switch r.Hold {
		case "", "none", "busy":
		default:
			errs = append(errs, fmt.Errorf("%s: hold %q (want none or busy)", at, r.Hold))
		}
		if r.MaxHold < 0 || r.SuspendFor < 0 {
			errs = append(errs, fmt.Errorf("%s: negative duration", at))
		}
		if g := r.Guard; g != nil {
			if g.MinRequests <= 0 || g.MaxErrorRatio <= 0 || g.MaxErrorRatio > 1 || g.Window <= 0 {
				errs = append(errs, fmt.Errorf("%s: guard needs min_requests > 0, 0 < max_error_ratio <= 1 and window > 0", at))
			}
		}
		for k, vs := range r.Match.Attrs {
			if k == "" || len(vs) == 0 {
				errs = append(errs, fmt.Errorf("%s: attrs[%q] needs a key and at least one value", at, k))
			}
		}
		for _, v := range r.Match.OCPP {
			if n := normVersion(v); n != "1.6" && n != "2.0.1" && n != "2.1" {
				errs = append(errs, fmt.Errorf("%s: unknown OCPP version %q", at, v))
			}
		}
		if len(r.Actions) > 0 && !r.AllowSplit {
			if err := checkSplit(r); err != nil {
				errs = append(errs, fmt.Errorf("%s: %w", at, err))
			}
		}
	}
	return errors.Join(errs...)
}

// checkSplit refuses an action set that routes some but not all members of a
// transaction group of an OCPP version the rule can match.
func checkSplit(r Rule) error {
	acts := map[string]bool{}
	for _, a := range r.Actions {
		acts[a] = true
	}
	var vers []string
	for _, v := range r.Match.OCPP {
		vers = append(vers, normVersion(v))
	}
	inPlay := func(match func(string) bool, specific ...string) bool {
		if len(vers) > 0 {
			return slices.ContainsFunc(vers, match)
		}
		// No version restriction: the group is in play when the rule names an
		// action only that group has, or only shared ones (which both have).
		for _, s := range specific {
			if acts[s] {
				return true
			}
		}
		return !acts["StartTransaction"] && !acts["StopTransaction"] && !acts["TransactionEvent"]
	}
	check := func(name string, group []string) error {
		hit, missing := false, []string{}
		for _, a := range group {
			if acts[a] {
				hit = true
			} else {
				missing = append(missing, a)
			}
		}
		if hit && len(missing) > 0 {
			return fmt.Errorf("actions split the %s transaction group (missing %s); add them or set allow_split", name, strings.Join(missing, ", "))
		}
		return nil
	}
	var errs []error
	if inPlay(isV16, "StartTransaction", "StopTransaction") {
		errs = append(errs, check("OCPP 1.6", groupV16))
	}
	if inPlay(isV2x, "TransactionEvent") {
		errs = append(errs, check("OCPP 2.x", groupV2x))
	}
	return errors.Join(errs...)
}

// Bucket returns the percentage bucket (0..9999) of a charge point:
// uint64_be(SHA-256(salt || 0x00 || cpID)[0:8]) mod 10000. A charger is in a
// rule at percent p when Bucket < p*100, so raising p only adds chargers.
func Bucket(salt, cpID string) uint64 {
	h := sha256.New()
	h.Write([]byte(salt))
	h.Write([]byte{0})
	h.Write([]byte(cpID))
	var sum [sha256.Size]byte
	return binary.BigEndian.Uint64(h.Sum(sum[:0])[:8]) % 10000
}
