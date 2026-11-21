package admin

import (
	"math"
	"sort"
)

// Load is one gateway's session count as seen in its lease.
type Load struct {
	ID       string
	Sessions int
	// Serving is false for a draining gateway: it is neither a target nor a
	// source of a rebalance.
	Serving bool
}

// Shed is one step of a rebalance plan.
type Shed struct {
	ID    string
	Count int
}

// FairShare is the sessions per serving gateway.
func FairShare(gws []Load) float64 {
	total, n := 0, 0
	for _, g := range gws {
		if g.Serving {
			total += g.Sessions
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return float64(total) / float64(n)
}

// PlanRebalance returns the sheds that bring every serving gateway above
// fair*tolerance back to its fair share, largest excess first. Gateways at or
// below the threshold are left alone, so a rebalance never moves sessions for
// a small imbalance (tolerance 1.1 means 10%). Chargers displaced by a shed
// land on whichever gateway the load balancer picks among the ready ones, so a
// plan is a step: run it, wait, plan again.
func PlanRebalance(gws []Load, tolerance float64) []Shed {
	if tolerance < 1 {
		tolerance = 1
	}
	fair := FairShare(gws)
	if !Balanced(gws, tolerance) {
		// Somebody is far below its share (a fresh canary, a restarted pod) while
		// nobody may be far enough above to cross the threshold: shed from
		// everything above the fair share, or the plan would stay empty while the
		// fleet is uneven.
		tolerance = 1
	}
	var out []Shed
	for _, g := range gws {
		if !g.Serving || float64(g.Sessions) <= fair*tolerance {
			continue
		}
		if n := g.Sessions - int(math.Round(fair)); n > 0 {
			out = append(out, Shed{ID: g.ID, Count: n})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// Balanced reports whether every serving gateway is within tolerance of the fair
// share, both ways (a gateway far below it is the cost of the ones above).
func Balanced(gws []Load, tolerance float64) bool {
	fair := FairShare(gws)
	for _, g := range gws {
		if !g.Serving {
			continue
		}
		if float64(g.Sessions) > fair*tolerance || float64(g.Sessions) < fair/tolerance {
			return false
		}
	}
	return true
}
