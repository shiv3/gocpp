package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Rollout scenarios: timed hook commands (gocppctl route apply / rollout run),
// per-pool backend arguments (an error-injecting canary) and a raw dump of the
// gateways' routing metrics. The analysis is in rollout_report.py.

// hookList is the repeatable -hook flag: "OFFSET=COMMAND".
type hookList []string

func (h *hookList) String() string     { return strings.Join(*h, "; ") }
func (h *hookList) Set(v string) error { *h = append(*h, v); return nil }

// poolArgsFor returns the extra arguments configured for pool in "pool=args;pool=args".
func poolArgsFor(spec, pool string) string {
	for _, part := range strings.Split(spec, ";") {
		name, args, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && name == pool {
			return args
		}
	}
	return ""
}

// startHooks schedules the -hook commands relative to now (the load starts).
func (g *gen) startHooks() {
	if len(g.cfg.Hooks) == 0 || g.topo == nil {
		return
	}
	var metrics, admin []string
	for _, gw := range g.topo.gws {
		metrics = append(metrics, "http://"+gw.admin+"/metrics")
		admin = append(admin, gw.admin)
	}
	env := append(os.Environ(), "LT_REDIS="+g.topo.redis.Addr(), "LT_OUT="+g.cfg.Out,
		"LT_GW_METRICS="+strings.Join(metrics, ","), "LT_GW_ADMIN="+strings.Join(admin, ","))
	for i, spec := range g.cfg.Hooks {
		off, cmdline, ok := strings.Cut(spec, "=")
		d, err := time.ParseDuration(off)
		if !ok || err != nil {
			g.events.add("hook_error", "bad -hook "+spec)
			continue
		}
		i, cmdline := i, cmdline
		time.AfterFunc(d, func() {
			if g.ctx.Err() != nil {
				return
			}
			g.events.add("hook_start", fmt.Sprintf("%d: %s", i, cmdline))
			lf, _ := os.Create(filepath.Join(g.cfg.Out, fmt.Sprintf("hook-%d.log", i)))
			defer func() { _ = lf.Close() }()
			cmd := exec.Command("sh", "-c", cmdline)
			cmd.Env, cmd.Stdout, cmd.Stderr = env, lf, lf
			// A hook may run for a long time (rollout run); it ends with the load.
			go func() { <-g.ctx.Done(); _ = cmd.Process.Kill() }()
			err := cmd.Run()
			g.events.add("hook_end", fmt.Sprintf("%d: err=%v", i, err))
		})
	}
}

// gatewayMetricsSampler appends the routing-related gateway metrics to
// gwmetrics.csv every sample interval: t_s,gateway,metric,labels,value.
func (g *gen) gatewayMetricsSampler() {
	if g.topo == nil {
		return
	}
	f, err := os.Create(filepath.Join(g.cfg.Out, "gwmetrics.csv"))
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	w := bufio.NewWriter(f)
	defer func() { _ = w.Flush() }()
	fmt.Fprintln(w, "t_s,gateway,metric,labels,value")
	keep := []string{"gocpp_gateway_upstream_requests_total", "gocpp_gateway_upstream_seconds_", "gocpp_gateway_route_",
		"gocpp_gateway_late_replies_total", "gocpp_gateway_orphan_results_total", "gocpp_gateway_pool_members", "gocpp_connections"}
	tick := time.NewTicker(g.cfg.Sample)
	defer tick.Stop()
	cl := &http.Client{Timeout: 3 * time.Second}
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-tick.C:
		}
		secs := g.since()
		for _, gw := range g.topo.gws {
			resp, err := cl.Get("http://" + gw.admin + "/metrics")
			if err != nil {
				continue
			}
			sc := bufio.NewScanner(resp.Body)
			sc.Buffer(make([]byte, 1<<20), 1<<20)
			for sc.Scan() {
				l := sc.Text()
				if l == "" || l[0] == '#' {
					continue
				}
				ok := false
				for _, k := range keep {
					if strings.HasPrefix(l, k) {
						ok = true
						break
					}
				}
				if !ok {
					continue
				}
				i := strings.LastIndexByte(l, ' ')
				if i < 0 {
					continue
				}
				name, labels := l[:i], ""
				if j := strings.IndexByte(name, '{'); j >= 0 {
					name, labels = name[:j], strings.Trim(l[j:i], "{}")
				}
				if _, err := strconv.ParseFloat(l[i+1:], 64); err != nil {
					continue
				}
				fmt.Fprintf(w, "%.0f,%s,%s,\"%s\",%s\n", secs, gw.name, name, strings.ReplaceAll(labels, `"`, `'`), l[i+1:])
			}
			_ = resp.Body.Close()
		}
		_ = w.Flush()
	}
}
