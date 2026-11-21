package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway/admin"
)

// gatewayCmd implements "gocppctl gateway ...": staged releases of the
// connection tier (docs/staged-releases.md, docs/rollout.md). Gateways are found
// in the registry leases; each lease carries the address of its admin listener in
// the label "admin" (gocpp-gateway --advertise-admin).
func gatewayCmd(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("gateway: subcommand required (status, shed, cap, rebalance)")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("gateway "+sub, flag.ContinueOnError)
	var c conn
	c.flags(fs)
	var (
		token     = fs.String("admin-token", os.Getenv("GOCPPCTL_ADMIN_TOKEN"), "bearer token of the gateways' admin API")
		gwID      = fs.String("gateway", "", "gateway id (the lease id, the pod name)")
		adminAddr = fs.String("admin-addr", "", "admin host:port, instead of the lease label (single gateway)")
		count     = fs.Int("count", 0, "shed: sessions to close")
		minAge    = fs.Duration("min-age", 0, "shed/rebalance: skip sessions younger than this (default 10m; negative = no check)")
		rate      = fs.Float64("rate", 0, "shed/rebalance: sessions closed per second (default: spread over --window)")
		window    = fs.Duration("window", 0, "shed/rebalance: time to spread the closes over (default 5m)")
		wait      = fs.Bool("wait", true, "shed: wait until the gateway has finished shedding")
		live      = fs.Bool("live", false, "status: also ask each gateway's admin API")
		maxN      = fs.Int("max", 0, "cap: absolute soft cap in sessions")
		share     = fs.Float64("share", 0, "cap: soft cap as a percentage of the fleet's sessions (the canary's share)")
		off       = fs.Bool("off", false, "cap: remove the soft cap")
		tol       = fs.Float64("tolerance", 1.1, "rebalance: shed from gateways above fair share x tolerance")
		rounds    = fs.Int("rounds", 3, "rebalance: plan-shed-settle rounds at most")
		settle    = fs.Duration("settle", 30*time.Second, "rebalance: wait this long after a round for the chargers to reconnect")
		dry       = fs.Bool("dry-run", false, "rebalance: print the plan, change nothing")
	)
	var attrs multiFlag
	fs.Var(&attrs, "attr", "shed/rebalance: only sessions with attribute key=value (repeatable)")
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) > 0 {
		return fmt.Errorf("gateway %s: unexpected argument %q", sub, pos[0])
	}
	reg, closeFn, err := c.registry()
	if err != nil {
		return err
	}
	defer closeFn()
	cl := admin.Client{Token: *token}
	g := &gwTool{ctx: ctx, reg: reg, cl: cl, out: out, override: *adminAddr}
	attrMap, err := attrs.m()
	if err != nil {
		return err
	}
	shed := admin.ShedRequest{MinAge: admin.Duration(*minAge), Attrs: attrMap, Rate: *rate, Window: admin.Duration(*window)}

	switch sub {
	case "status":
		return g.status(*live)
	case "shed":
		if *gwID == "" || *count <= 0 {
			return errors.New("gateway shed: --gateway and --count are required")
		}
		shed.Count = *count
		return g.shed(*gwID, shed, *wait)
	case "cap":
		if *gwID == "" {
			return errors.New("gateway cap: --gateway is required")
		}
		return g.cap(*gwID, *maxN, *share, *off)
	case "rebalance":
		return g.rebalance(shed, *tol, *rounds, *settle, *dry)
	}
	return fmt.Errorf("gateway: unknown subcommand %q", sub)
}

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func (m multiFlag) m() (map[string]string, error) {
	if len(m) == 0 {
		return nil, nil
	}
	out := map[string]string{}
	for _, kv := range m {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			return nil, fmt.Errorf("--attr %q: want key=value", kv)
		}
		out[k] = v
	}
	return out, nil
}

type gwTool struct {
	ctx      context.Context
	reg      storage.SessionRegistry
	cl       admin.Client
	out      io.Writer
	override string
}

func (g *gwTool) leases() ([]storage.GatewayInfo, error) {
	gws, err := g.reg.Gateways(g.ctx)
	if err != nil {
		return nil, err
	}
	sort.Slice(gws, func(i, j int) bool { return gws[i].ID < gws[j].ID })
	return gws, nil
}

func (g *gwTool) find(id string) (storage.GatewayInfo, error) {
	gws, err := g.leases()
	if err != nil {
		return storage.GatewayInfo{}, err
	}
	for _, x := range gws {
		if x.ID == id {
			return x, nil
		}
	}
	return storage.GatewayInfo{}, fmt.Errorf("no gateway %q holds a lease (known: %s)", id, ids(gws))
}

func ids(gws []storage.GatewayInfo) string {
	var s []string
	for _, g := range gws {
		s = append(s, g.ID)
	}
	return strings.Join(s, ", ")
}

func (g *gwTool) addr(gi storage.GatewayInfo) (string, error) {
	if g.override != "" {
		return g.override, nil
	}
	if a := gi.Labels["admin"]; a != "" {
		return a, nil
	}
	return "", fmt.Errorf("gateway %q published no admin address (start it with --advertise-admin, or pass --admin-addr)", gi.ID)
}

func (g *gwTool) status(live bool) error {
	gws, err := g.leases()
	if err != nil {
		return err
	}
	total := 0
	for _, x := range gws {
		total += x.Sessions
	}
	tw := tabwriter.NewWriter(g.out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "GATEWAY\tSTATE\tVERSION\tSESSIONS\tSHARE\tPOLICY REV\tSUSPENDED\tADMIN\tLIVE")
	for _, x := range gws {
		share := "-"
		if total > 0 {
			share = fmt.Sprintf("%.1f%%", 100*float64(x.Sessions)/float64(total))
		}
		liveCol := ""
		if live {
			liveCol = "unreachable"
			if a, err := g.addr(x); err == nil {
				if st, err := g.cl.Status(g.ctx, a); err == nil {
					liveCol = fmt.Sprintf("sessions=%d soft_max=%d shedding=%t", st.Sessions, st.SoftMax, st.PartialDrain)
				}
			}
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\t%s\t%s\t%s\t%s\n", x.ID, x.State, x.Labels["version"], x.Sessions, share,
			x.Labels["policy_rev"], x.Labels["suspended"], x.Labels["admin"], liveCol)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	fmt.Fprintf(g.out, "%d gateways, %d sessions, fair share %.1f\n", len(gws), total, admin.FairShare(loads(gws)))
	return nil
}

func loads(gws []storage.GatewayInfo) []admin.Load {
	out := make([]admin.Load, 0, len(gws))
	for _, x := range gws {
		out = append(out, admin.Load{ID: x.ID, Sessions: x.Sessions, Serving: x.State != storage.GatewayDraining})
	}
	return out
}

func (g *gwTool) shed(id string, req admin.ShedRequest, wait bool) error {
	gi, err := g.find(id)
	if err != nil {
		return err
	}
	a, err := g.addr(gi)
	if err != nil {
		return err
	}
	st, err := g.cl.Shed(g.ctx, a, req)
	if err != nil {
		return err
	}
	fmt.Fprintf(g.out, "%s: shedding %d of %d sessions\n", id, req.Count, st.Sessions)
	if !wait {
		return nil
	}
	st, err = g.cl.WaitShed(g.ctx, a, 500*time.Millisecond)
	if err != nil {
		return err
	}
	fmt.Fprintf(g.out, "%s: done, %d sessions left (closed %d)\n", id, st.Sessions, st.Drain.Closed)
	return nil
}

func (g *gwTool) cap(id string, maxN int, share float64, off bool) error {
	n := maxN
	switch {
	case off:
		n = 0
	case share > 0:
		gws, err := g.leases()
		if err != nil {
			return err
		}
		total := 0
		for _, x := range gws {
			total += x.Sessions
		}
		n = int(math.Ceil(share / 100 * float64(total)))
		if n < 1 {
			n = 1
		}
		fmt.Fprintf(g.out, "fleet has %d sessions: %.1f%% = %d\n", total, share, n)
	case maxN <= 0:
		return errors.New("gateway cap: one of --max, --share or --off is required")
	}
	gi, err := g.find(id)
	if err != nil {
		return err
	}
	a, err := g.addr(gi)
	if err != nil {
		return err
	}
	st, err := g.cl.SetSoftMax(g.ctx, a, n)
	if err != nil {
		return err
	}
	if st.SoftMax == 0 {
		fmt.Fprintf(g.out, "%s: soft cap removed\n", id)
	} else {
		fmt.Fprintf(g.out, "%s: soft cap %d (it reports not-ready at or above it and keeps its %d sessions)\n", id, st.SoftMax, st.Sessions)
	}
	return nil
}

func (g *gwTool) rebalance(shed admin.ShedRequest, tol float64, rounds int, settle time.Duration, dry bool) error {
	for round := 1; round <= rounds; round++ {
		gws, err := g.leases()
		if err != nil {
			return err
		}
		ld := loads(gws)
		plan := admin.PlanRebalance(ld, tol)
		fmt.Fprintf(g.out, "round %d: %d serving gateways, fair share %.1f\n", round, countServing(ld), admin.FairShare(ld))
		if len(plan) == 0 {
			fmt.Fprintln(g.out, "balanced: every gateway is within tolerance")
			return nil
		}
		for _, p := range plan {
			fmt.Fprintf(g.out, "  shed %d from %s\n", p.Count, p.ID)
		}
		if dry {
			return nil
		}
		for _, p := range plan {
			req := shed
			req.Count = p.Count
			if err := g.shed(p.ID, req, true); err != nil {
				return err
			}
		}
		if round == rounds {
			break
		}
		select {
		case <-g.ctx.Done():
			return g.ctx.Err()
		case <-time.After(settle):
		}
	}
	gws, err := g.leases()
	if err != nil {
		return err
	}
	if admin.Balanced(loads(gws), tol) {
		fmt.Fprintln(g.out, "balanced")
		return nil
	}
	return &exitError{exitNotConverge, errors.New("not balanced after the last round; run again, or wait for the chargers to reconnect")}
}

func countServing(l []admin.Load) int {
	n := 0
	for _, x := range l {
		if x.Serving {
			n++
		}
	}
	return n
}
