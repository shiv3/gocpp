package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"github.com/shiv3/gocpp/addons/rollout"
	"github.com/shiv3/gocpp/gateway/routepolicy"
)

// rolloutCmd implements "gocppctl rollout": the optional automatic promotion of
// a backend canary (addons/rollout). Nothing runs unless you start "run"; the
// manual path (gocppctl route apply / rollback) is unaffected.
func rolloutCmd(ctx context.Context, args []string, out io.Writer) error {
	if len(args) == 0 {
		return errors.New("rollout: subcommand required (validate, run)")
	}
	sub, args := args[0], args[1:]
	fs := flag.NewFlagSet("rollout "+sub, flag.ContinueOnError)
	var c conn
	if sub == "run" {
		c.flags(fs)
	}
	var (
		dry        = fs.Bool("dry-run", false, "evaluate and report, write nothing (stages advance virtually)")
		promURL    = fs.String("prometheus", "", "Prometheus base URL for the gate queries (supports application checks)")
		promToken  = fs.String("prometheus-token", os.Getenv("GOCPPCTL_PROMETHEUS_TOKEN"), "bearer token for Prometheus")
		scrape     = fs.String("scrape", "", "comma separated gateway /metrics URLs to scrape instead of Prometheus (default: every gateway lease's admin address)")
		kubeEvents = fs.String("kube-events", "", "also post Kubernetes events about KIND/NAME (e.g. Deployment/backend-canary); needs an in-cluster service account")
	)
	pos, err := parseInterspersed(fs, args)
	if err != nil {
		return err
	}
	if len(pos) != 1 {
		return fmt.Errorf("rollout %s: exactly one plan FILE", sub)
	}
	data, err := os.ReadFile(pos[0])
	if err != nil {
		return err
	}
	plan, err := rollout.Parse(data)
	if err != nil {
		return err
	}
	switch sub {
	case "validate":
		fmt.Fprintf(out, "ok: rule %q through %d stages\n", plan.Rule, len(plan.Stages))
		return nil
	case "run":
	default:
		return fmt.Errorf("rollout: unknown subcommand %q", sub)
	}

	reg, closeFn, err := c.registry()
	if err != nil {
		return err
	}
	defer closeFn()
	var src rollout.Source
	switch {
	case *promURL != "":
		src = &rollout.PromSource{URL: *promURL, Token: *promToken}
	default:
		targets := splitList(*scrape)
		if len(targets) == 0 {
			gws, err := reg.Gateways(ctx)
			if err != nil {
				return err
			}
			for _, g := range gws {
				if a := g.Labels["admin"]; a != "" {
					targets = append(targets, "http://"+a+"/metrics")
				}
			}
		}
		if len(targets) == 0 {
			return errors.New("rollout run: no metrics source: pass --prometheus or --scrape, or start the gateways with --advertise-admin")
		}
		src = &rollout.ScrapeSource{Targets: targets}
	}
	notifiers := []rollout.Notifier{rollout.LogNotifier(slog.New(slog.NewTextHandler(out, nil)))}
	if *kubeEvents != "" {
		kind, name, ok := strings.Cut(*kubeEvents, "/")
		if !ok {
			return errors.New("--kube-events wants KIND/NAME")
		}
		k, err := rollout.InClusterKubeEvents(rollout.ObjectRef{Kind: kind, Name: name})
		if err != nil {
			return err
		}
		notifiers = append(notifiers, k)
	}
	ctl := rollout.New(plan, routepolicy.Controller{Store: reg, Registry: reg}, src, rollout.Multi(notifiers...))
	ctl.DryRun = *dry
	if *dry {
		fmt.Fprintln(out, "dry run: nothing will be written")
	}
	if err := ctl.Run(ctx); err != nil {
		return err
	}
	switch ctl.State() {
	case rollout.StateDone:
		return nil
	case rollout.StateRolledBack:
		return &exitError{exitErr, errors.New("rolled back")}
	default:
		return &exitError{exitNotConverge, fmt.Errorf("rollout ended in state %s", ctl.State())}
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
