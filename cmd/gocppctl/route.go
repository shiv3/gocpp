package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"gopkg.in/yaml.v3"
)

func routeCmd(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("route: subcommand required (validate, show, apply, rollback, status)")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("route "+sub, flag.ContinueOnError)
	var c conn
	var (
		wait       = fs.Duration("wait", 30*time.Second, "wait this long for every gateway to report the revision (0 = do not wait)")
		expect     = fs.Uint64("expect-rev", 0, "apply: fail unless the stored revision is this (compare-and-set)")
		force      = fs.Bool("force", false, "apply: skip the route.v1 capability check")
		known      = fs.String("known-pools", "", "apply/validate: comma separated pools the policy may name")
		to         = fs.Uint64("to", 0, "rollback: revision to restore (default: the one before the current)")
		history    = fs.Bool("history", false, "show: list the stored revisions")
		format     = fs.String("o", "yaml", "show: yaml or json")
		needsStore = sub != "validate"
	)
	if needsStore {
		c.flags(fs)
	}
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	var pools []string
	if *known != "" {
		pools = strings.Split(*known, ",")
	}

	if sub == "validate" {
		if len(pos) != 1 {
			return errors.New("route validate: exactly one FILE")
		}
		return validateFile(pos[0], pools, out)
	}
	reg, closeFn, err := c.registry()
	if err != nil {
		return err
	}
	defer closeFn()
	ctl := routepolicy.Controller{Store: reg, Registry: reg}

	switch sub {
	case "show":
		return show(ctx, ctl, *history, *format, out)
	case "status":
		st, err := ctl.Status(ctx)
		if err != nil {
			return err
		}
		printStatus(out, st)
		if !st.Converged {
			return &exitError{exitNotConverge, fmt.Errorf("revision %d is not on every gateway", st.Revision)}
		}
		return nil
	case "apply":
		if len(pos) != 1 {
			return errors.New("route apply: exactly one FILE")
		}
		data, err := os.ReadFile(pos[0])
		if err != nil {
			return err
		}
		p, err := routepolicy.Parse(data)
		if err != nil {
			return err
		}
		rev, err := ctl.Put(ctx, p, routepolicy.PutOptions{ExpectRev: *expect, Force: *force, KnownPools: pools})
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "revision %d written\n", rev)
		return waitAndReport(ctx, ctl, rev, *wait, out)
	case "rollback":
		rev, restored, err := ctl.Rollback(ctx, *to)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "revision %d written (contents of revision %d, holds ignored)\n", rev, restored)
		return waitAndReport(ctx, ctl, rev, *wait, out)
	}
	return fmt.Errorf("route: unknown subcommand %q", sub)
}

// parseInterspersed parses flags that may come before or after positional
// arguments ("route apply FILE --wait 5s") and returns the positionals.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return pos, nil
		}
		pos = append(pos, fs.Arg(0))
		args = fs.Args()[1:]
	}
}

func validateFile(path string, pools []string, out io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	p, err := routepolicy.Parse(data)
	if err != nil {
		return err
	}
	if err := p.Validate(); err != nil {
		return err
	}
	if len(pools) > 0 {
		known := map[string]bool{}
		for _, n := range pools {
			known[n] = true
		}
		for _, n := range p.Pools() {
			if !known[n] {
				return fmt.Errorf("pool %q is not one of the known pools %v", n, pools)
			}
		}
	}
	fmt.Fprintf(out, "ok: %s; pools %s\n", routepolicy.Describe(p), strings.Join(p.Pools(), ", "))
	return nil
}

func waitAndReport(ctx context.Context, ctl routepolicy.Controller, rev uint64, wait time.Duration, out io.Writer) error {
	if wait <= 0 {
		return nil
	}
	st, err := ctl.WaitConverged(ctx, rev, wait)
	if err != nil {
		printStatus(out, st)
		return &exitError{exitNotConverge, err}
	}
	fmt.Fprintf(out, "revision %d is active on %d/%d gateways\n", rev, len(st.Gateways), len(st.Gateways))
	return nil
}

func show(ctx context.Context, ctl routepolicy.Controller, history bool, format string, out io.Writer) error {
	if history {
		h, ok := ctl.Store.(storage.PolicyHistory)
		if !ok {
			return errors.New("the store keeps no history")
		}
		es, err := h.PolicyHistory(ctx)
		if err != nil {
			return err
		}
		tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
		fmt.Fprintln(tw, "REVISION\tWRITTEN\tSUMMARY")
		for _, e := range es {
			summary := "unparseable"
			if p, err := routepolicy.Parse(e.Data); err == nil {
				p.Revision = e.Rev
				summary = routepolicy.Describe(p)
			}
			fmt.Fprintf(tw, "%d\t%s\t%s\n", e.Rev, e.At.Format(time.RFC3339), summary)
		}
		return tw.Flush()
	}
	p, err := ctl.Current(ctx)
	if err != nil {
		return err
	}
	if p.Revision == 0 {
		fmt.Fprintln(out, "no policy stored (gateways route everything to the default pool)")
		return nil
	}
	b, err := routepolicy.Marshal(p)
	if err != nil {
		return err
	}
	if format == "json" {
		_, err = fmt.Fprintln(out, string(b))
		return err
	}
	var v any
	if err := yaml.Unmarshal(b, &v); err != nil {
		return err
	}
	y, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = out.Write(y)
	return err
}

func printStatus(out io.Writer, st routepolicy.Status) {
	on := 0
	for _, g := range st.Gateways {
		if g.PolicyRev == st.Revision {
			on++
		}
	}
	fmt.Fprintf(out, "stored revision %d, active on %d/%d gateways\n", st.Revision, on, len(st.Gateways))
	tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "GATEWAY\tSTATE\tVERSION\tPOLICY REV\tSUSPENDED\tSESSIONS")
	for _, g := range st.Gateways {
		mark := ""
		if g.PolicyRev != st.Revision {
			mark = " *"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%d%s\t%s\t%d\n", g.ID, g.State, g.Version, g.PolicyRev, mark, strings.Join(g.Suspended, ","), g.Sessions)
	}
	_ = tw.Flush()
}
