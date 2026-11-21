package routepolicy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway"
)

// ErrConflict is returned when another writer changed the policy between the
// read and the compare-and-set.
var ErrConflict = errors.New("routepolicy: the policy changed concurrently (compare-and-set failed); re-read and retry")

// ErrNotConverged is returned by Controller.WaitConverged when some gateways
// have not reported the revision in time.
var ErrNotConverged = errors.New("routepolicy: not every gateway runs the revision")

// Controller writes policy revisions to a storage.PolicyStore and reads back
// what the gateways report in their leases. It is what gocppctl route and the
// automatic rollout controller (addons/rollout) use.
type Controller struct {
	Store storage.PolicyStore
	// Registry supplies the gateway leases for status, the capability check and
	// convergence. May be nil (then those are skipped and convergence is
	// reported as unknown).
	Registry storage.SessionRegistry
}

// PutOptions tunes Controller.Put.
type PutOptions struct {
	// ExpectRev, when non-zero, makes the write fail with ErrConflict unless the
	// stored revision equals it.
	ExpectRev uint64
	// IgnoreHold marks the revision as a rollback (hold: busy is ignored).
	IgnoreHold bool
	// Force skips the capability check.
	Force bool
	// KnownPools, when non-empty, makes a policy naming another pool an error.
	KnownPools []string
}

// GatewayStatus is one gateway as seen in its lease.
type GatewayStatus struct {
	ID        string
	State     string
	Version   string
	PolicyRev uint64
	Suspended []string
	Caps      []string
	Sessions  int
}

// Status is the policy and the gateways' view of it.
type Status struct {
	Revision uint64
	Gateways []GatewayStatus
	// Converged is true when Registry is set and every gateway reports Revision.
	Converged bool
}

// Lagging lists the gateways not on the revision.
func (s Status) Lagging() []string {
	var out []string
	for _, g := range s.Gateways {
		if g.PolicyRev != s.Revision {
			out = append(out, fmt.Sprintf("%s (rev %d)", g.ID, g.PolicyRev))
		}
	}
	return out
}

// Current returns the stored policy (zero revision when none).
func (c Controller) Current(ctx context.Context) (Policy, error) {
	rev, data, err := c.Store.Policy(ctx)
	if err != nil || rev == 0 {
		return Policy{}, err
	}
	p, err := Parse(data)
	if err != nil {
		return Policy{}, fmt.Errorf("stored revision %d: %w", rev, err)
	}
	p.Revision = rev
	return p, nil
}

// Put validates p and stores it as the next revision (the stored revision + 1,
// or p.Revision when that is higher), with compare-and-set. It returns the
// revision written.
func (c Controller) Put(ctx context.Context, p Policy, o PutOptions) (uint64, error) {
	if err := p.Validate(); err != nil {
		return 0, err
	}
	if len(o.KnownPools) > 0 {
		known := map[string]bool{}
		for _, n := range o.KnownPools {
			known[n] = true
		}
		for _, n := range p.Pools() {
			if !known[n] {
				return 0, fmt.Errorf("pool %q is not one of the known pools %v", n, o.KnownPools)
			}
		}
	}
	cur, _, err := c.Store.Policy(ctx)
	if err != nil {
		return 0, err
	}
	if o.ExpectRev != 0 && cur != o.ExpectRev {
		return 0, fmt.Errorf("%w (stored revision %d, expected %d)", ErrConflict, cur, o.ExpectRev)
	}
	if !o.Force && len(p.Rules) > 0 && c.Registry != nil {
		gws, err := c.Registry.Gateways(ctx)
		if err != nil {
			return 0, err
		}
		if err := CheckCapabilities(gws); err != nil {
			return 0, err
		}
	}
	rev := max(cur+1, p.Revision)
	p.Revision = rev
	p.IgnoreHold = o.IgnoreHold
	data, err := Marshal(p)
	if err != nil {
		return 0, err
	}
	ok, err := c.Store.PutPolicy(ctx, cur, rev, data)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrConflict
	}
	return rev, nil
}

// CheckCapabilities refuses a policy with rules while a gateway lease lacks the
// route.v1 capability: that gateway would send everything to the default pool
// and the staged release would silently not happen there.
func CheckCapabilities(gws []storage.GatewayInfo) error {
	var missing []string
	for _, g := range gws {
		if !hasCap(g.Labels["caps"], gateway.CapRouteV1) {
			missing = append(missing, g.ID)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("gateways without the %s capability (they ignore routing policies): %s", gateway.CapRouteV1, strings.Join(missing, ", "))
	}
	return nil
}

func hasCap(list, c string) bool {
	for _, x := range strings.Split(list, ",") {
		if strings.TrimSpace(x) == c {
			return true
		}
	}
	return false
}

// Rollback writes the policy of revision to (or, when to is 0, of the revision
// before the current one) as a new revision with IgnoreHold set, so every
// session moves back at once. It needs a store that keeps history.
func (c Controller) Rollback(ctx context.Context, to uint64) (newRev, restored uint64, err error) {
	h, ok := c.Store.(storage.PolicyHistory)
	if !ok {
		return 0, 0, errors.New("routepolicy: the policy store keeps no history")
	}
	hist, err := h.PolicyHistory(ctx)
	if err != nil {
		return 0, 0, err
	}
	if len(hist) == 0 {
		return 0, 0, errors.New("routepolicy: no policy stored")
	}
	var target *storage.PolicyEntry
	switch {
	case to != 0:
		for i := range hist {
			if hist[i].Rev == to {
				target = &hist[i]
			}
		}
		if target == nil {
			return 0, 0, fmt.Errorf("routepolicy: revision %d is not in the history", to)
		}
	case len(hist) >= 2:
		target = &hist[1]
	default:
		return 0, 0, errors.New("routepolicy: no earlier revision to roll back to")
	}
	p, err := Parse(target.Data)
	if err != nil {
		return 0, 0, err
	}
	p.Revision = 0
	// Rollback must not be blocked by the capability check: it only removes
	// routing.
	rev, err := c.Put(ctx, p, PutOptions{IgnoreHold: true, Force: true, ExpectRev: hist[0].Rev})
	return rev, target.Rev, err
}

// Status reads the stored revision and the gateway leases.
func (c Controller) Status(ctx context.Context) (Status, error) {
	rev, _, err := c.Store.Policy(ctx)
	if err != nil {
		return Status{}, err
	}
	st := Status{Revision: rev}
	if c.Registry == nil {
		return st, nil
	}
	gws, err := c.Registry.Gateways(ctx)
	if err != nil {
		return st, err
	}
	for _, g := range gws {
		pr, _ := strconv.ParseUint(g.Labels["policy_rev"], 10, 64)
		gs := GatewayStatus{ID: g.ID, State: g.State, Version: g.Labels["version"], PolicyRev: pr, Sessions: g.Sessions}
		if s := g.Labels["suspended"]; s != "" {
			gs.Suspended = strings.Split(s, ",")
		}
		if s := g.Labels["caps"]; s != "" {
			gs.Caps = strings.Split(s, ",")
		}
		st.Gateways = append(st.Gateways, gs)
	}
	sort.Slice(st.Gateways, func(i, j int) bool { return st.Gateways[i].ID < st.Gateways[j].ID })
	st.Converged = len(st.Lagging()) == 0
	return st, nil
}

// WaitConverged polls until every gateway lease reports rev (or higher), and
// returns the last Status. On timeout it returns ErrNotConverged with the
// lagging gateways named.
func (c Controller) WaitConverged(ctx context.Context, rev uint64, timeout time.Duration) (Status, error) {
	if c.Registry == nil {
		return Status{}, fmt.Errorf("%w: no registry to read the gateway leases from", ErrNotConverged)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	var last Status
	for {
		if st, err := c.Status(ctx); err == nil {
			last = st
			if lag := laggingBehind(st, rev); len(lag) == 0 {
				last.Converged = true
				return last, nil
			}
		}
		select {
		case <-ctx.Done():
			last.Converged = false
			return last, fmt.Errorf("%w: revision %d; lagging: %s", ErrNotConverged, rev, strings.Join(laggingBehind(last, rev), ", "))
		case <-t.C:
		}
	}
}

func laggingBehind(st Status, rev uint64) []string {
	var lag []string
	for _, g := range st.Gateways {
		if g.PolicyRev < rev {
			lag = append(lag, fmt.Sprintf("%s (rev %d)", g.ID, g.PolicyRev))
		}
	}
	return lag
}
