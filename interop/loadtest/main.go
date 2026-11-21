// Command loadtest is a large-fleet OCPP 1.6J load/soak generator and harness.
// It supervises a parity CSMS binary (cs-gocpp or cs-ocppgo), simulates N
// chargers (some mute, some flapping), issues CSMS-initiated calls through the
// control API, restarts the server on a schedule and records everything.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type step struct {
	N     int
	Rate  float64
	Dur   time.Duration
	Speed float64 // message-rate multiplier for heartbeat/meter timers (0 = 1)
}

type config struct {
	ServerBin, ServerArgs, Addr, Taskset, Pprof, Out, Label string
	ServerProcs                                             int
	Steps                                                   []step
	Ramp, HB, ReconnBase, ReconnCap, ReconnJitter0          time.Duration
	RestartGap, Sample, FreezeThresh, CallTimeout           time.Duration
	TxCycle, TxLen, Meter                                   time.Duration
	HBJitter, Mute, TxFrac                                  float64
	Flappers, Dups, Restarts, Connectors, MinAvailMB        int
	Attach                                                  bool
	GCTrace                                                 bool
	Hot                                                     int
	HotShare                                                float64
	// Split topology (see topology.go).
	GatewayBin, BackendPools, GwTaskset, BeTaskset, GwArgs, RestartTarget, RestartMode, Plan string
	LinkLinger                                                                               time.Duration
	Gateways                                                                                 int
	// Rollout scenarios (see rollout.go).
	PoolArgs   string
	HandledLog bool
	Hooks      hookList
}

type gen struct {
	cfg      config
	ctx      context.Context
	cancel   context.CancelFunc
	start    time.Time
	httpc    *http.Client
	ctlc     *http.Client
	up       atomic.Int64 // sessions past BootNotification
	started  atomic.Int64 // chargers launched
	curStep  atomic.Int64
	rate     atomic.Int64 // milli-calls per second
	speed    atomic.Int64 // milli speed factor
	msgs     atomic.Int64 // completed charger requests
	srv      *server
	topo     *topology
	met      *metrics
	canaries []*charger
	chargers []*charger
	events   *eventLog
	aborted  atomic.Bool
	abortWhy atomic.Value
}

func parseSteps(s string) ([]step, error) {
	var out []step
	for _, p := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(p), ":")
		if len(f) < 3 || len(f) > 4 {
			return nil, fmt.Errorf("bad step %q (want N:rate:dur[:speed])", p)
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			return nil, err
		}
		r, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			return nil, err
		}
		d, err := time.ParseDuration(f[2])
		if err != nil {
			return nil, err
		}
		sp := 1.0
		if len(f) == 4 {
			if sp, err = strconv.ParseFloat(f[3], 64); err != nil {
				return nil, err
			}
		}
		out = append(out, step{n, r, d, sp})
	}
	return out, nil
}

func main() {
	var c config
	var steps string
	flag.StringVar(&c.ServerBin, "server-bin", "", "CSMS binary (cs-gocpp or cs-ocppgo)")
	flag.StringVar(&c.ServerArgs, "server-args", "", "extra server args (space separated)")
	flag.StringVar(&c.Addr, "addr", "127.0.0.1:9000", "server listen address")
	flag.StringVar(&c.Taskset, "taskset", "", "cpu list for the server, e.g. 0-3")
	flag.IntVar(&c.ServerProcs, "server-procs", 4, "GOMAXPROCS for the server")
	flag.StringVar(&c.Pprof, "pprof", "127.0.0.1:6060", "server pprof address (passed as -pprof)")
	flag.StringVar(&c.Out, "out", "", "output directory")
	flag.StringVar(&c.Label, "label", "run", "label")
	flag.StringVar(&steps, "steps", "4000:50:25m", "comma list of N:callRatePerSec:duration")
	flag.DurationVar(&c.Ramp, "ramp", 60*time.Second, "connect ramp per step")
	flag.DurationVar(&c.HB, "hb", 30*time.Second, "heartbeat interval (compressed time)")
	flag.Float64Var(&c.HBJitter, "hb-jitter", 0.1, "heartbeat jitter fraction")
	flag.Float64Var(&c.Mute, "mute", 0.4, "fraction of mute chargers")
	flag.IntVar(&c.Flappers, "flappers", 3, "flapping chargers")
	flag.IntVar(&c.Dups, "dups", 20, "chargers run by two clients with the same id")
	flag.Float64Var(&c.TxFrac, "tx-frac", 0.15, "fraction of chargers doing transactions")
	flag.DurationVar(&c.TxCycle, "tx-cycle", 40*time.Second, "time between transaction starts")
	flag.DurationVar(&c.TxLen, "tx-len", 25*time.Second, "transaction length")
	flag.DurationVar(&c.Meter, "meter", 5*time.Second, "MeterValues interval")
	flag.IntVar(&c.Connectors, "connectors", 2, "connectors per charger")
	flag.IntVar(&c.Restarts, "restarts", 5, "planned server restarts (first step only)")
	flag.DurationVar(&c.RestartGap, "restart-gap", 2*time.Second, "pause between kill and start")
	flag.DurationVar(&c.ReconnBase, "reconnect-base", time.Second, "reconnect backoff base")
	flag.DurationVar(&c.ReconnCap, "reconnect-cap", 30*time.Second, "reconnect backoff cap")
	flag.DurationVar(&c.ReconnJitter0, "reconnect-jitter0", 3*time.Second, "initial reconnect jitter after a drop")
	flag.DurationVar(&c.CallTimeout, "call-timeout", 30*time.Second, "charger request timeout; also server call timeout")
	flag.DurationVar(&c.Sample, "sample", 5*time.Second, "sample interval")
	flag.DurationVar(&c.FreezeThresh, "freeze", 10*time.Second, "canary heartbeat RTT that counts as a freeze")
	flag.IntVar(&c.MinAvailMB, "min-avail-mb", 1200, "abort when system MemAvailable drops below this")
	flag.IntVar(&c.Hot, "hot", 0, "number of hot mute chargers that receive most CSMS calls (0 = uniform)")
	flag.Float64Var(&c.HotShare, "hot-share", 0.9, "share of CSMS calls aimed at the hot set")
	flag.StringVar(&c.GatewayBin, "gateway-bin", "", "cmd/gocpp-gateway binary (split topology)")
	flag.IntVar(&c.Gateways, "gateways", 0, "number of gateways; >0 runs the split topology (-server-bin must be cs-gocpp)")
	flag.StringVar(&c.BackendPools, "backend-pools", "default:2", "backend pools, name:count,... (the first backend serves /control at -addr)")
	flag.StringVar(&c.GwTaskset, "gw-taskset", "", "cpu list for each gateway")
	flag.StringVar(&c.BeTaskset, "be-taskset", "", "cpu list for each backend")
	flag.DurationVar(&c.LinkLinger, "link-write-linger", 0, "gRPC link write linger on both sides (gateways --link-write-linger, backends -link-write-linger); 0 = off")
	flag.StringVar(&c.GwArgs, "gw-args", "", "extra gateway args (space separated)")
	flag.StringVar(&c.RestartTarget, "restart-target", "server", "what -restarts acts on: server | backend | gateway | pool:NAME")
	flag.StringVar(&c.Plan, "plan", "", "mixed restart plan, comma separated target:mode (target server|backend|gateway|pool:NAME, mode term|kill), spread evenly over the first step; replaces -restarts")
	flag.StringVar(&c.RestartMode, "restart-mode", "term", "term (SIGTERM, drain) | kill (SIGKILL)")
	flag.StringVar(&c.PoolArgs, "pool-args", "", "extra backend args per pool, pool=args;pool=args (e.g. 'canary=-fail-rate 1')")
	flag.BoolVar(&c.HandledLog, "handled-log", false, "backends write handled-<name>.log (which charger each backend served; routing correctness)")
	flag.Var(&c.Hooks, "hook", "OFFSET=COMMAND, repeatable: run COMMAND (sh -c) OFFSET after the load starts; env LT_REDIS LT_OUT LT_GW_METRICS LT_GW_ADMIN; output in hook-N.log")
	flag.BoolVar(&c.Attach, "attach", false, "do not spawn the server")
	flag.BoolVar(&c.GCTrace, "gctrace", false, "start the server with GODEBUG=gctrace=1 (needed for the in-mark share in report.py)")
	knee := flag.Bool("knee", false, "preset: -steps 4000:50:150s:160,4000:50:150s:320 (about 30k then 60k requests/s; implies -gctrace and -restarts 0)")
	flag.Parse()
	if *knee {
		steps = "4000:50:150s:160,4000:50:150s:320"
		c.GCTrace = true
		c.Restarts = 0 // restarts inside the first step would dilute its message rate
	}
	var err error
	if c.Steps, err = parseSteps(steps); err != nil || c.Out == "" || (c.ServerBin == "" && !c.Attach) {
		log.Fatalf("usage: -server-bin B -out DIR [-steps ...]: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(c.Out, "dumps"), 0o755)
	debug.SetMemoryLimit(2 << 30)

	g := &gen{cfg: c, start: time.Now()}
	g.ctx, g.cancel = context.WithCancel(context.Background())
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	go func() { <-sig; log.Println("signal: stopping"); g.abort("signal") }()
	tr := &http.Transport{MaxIdleConns: 2000, MaxIdleConnsPerHost: 2000, IdleConnTimeout: 30 * time.Second}
	g.httpc = &http.Client{Transport: tr}
	g.ctlc = &http.Client{Transport: &http.Transport{MaxIdleConnsPerHost: 512}, Timeout: 90 * time.Second}
	g.met = newMetrics(g)
	g.events = newEventLog(filepath.Join(c.Out, "events.log"))
	g.run()
}

func (g *gen) abort(why string) {
	if g.aborted.CompareAndSwap(false, true) {
		g.abortWhy.Store(why)
		g.events.add("abort", why)
		g.cancel()
	}
}

func (g *gen) since() float64 { return time.Since(g.start).Seconds() }

func (g *gen) run() {
	c := g.cfg
	switch {
	case c.Gateways > 0:
		t, err := newTopology(g)
		if err != nil {
			log.Fatal(err)
		}
		g.topo = t
		if err := t.startAll(); err != nil {
			log.Fatal(err)
		}
	case !c.Attach:
		g.srv = newServer(g)
		if err := g.srv.start(); err != nil {
			log.Fatal(err)
		}
	}
	if !g.waitHealth(30 * time.Second) {
		log.Fatal("server did not become healthy")
	}
	g.start = time.Now()
	g.events.add("start", fmt.Sprintf("unix_ms=%d steps=%v", g.start.UnixMilli(), c.Steps))

	maxN := 0
	for _, s := range c.Steps {
		if s.N > maxN {
			maxN = s.N
		}
	}
	rng := rand.New(rand.NewSource(42))
	for i := 0; i < maxN; i++ {
		ch := newCharger(g, fmt.Sprintf("LT-%05d", i))
		ch.mute = rng.Float64() < c.Mute
		ch.tx = rng.Float64() < c.TxFrac
		g.chargers = append(g.chargers, ch)
	}
	// flappers and duplicate-id chargers are taken from the first non-mute ones
	nf, nd := 0, 0
	for _, ch := range g.chargers {
		if ch.mute {
			continue
		}
		if nf < c.Flappers {
			ch.flapper = true
			nf++
		} else if nd < c.Dups {
			ch.dup = true
			nd++
		}
	}
	for i := 1; i <= 2; i++ {
		cn := newCharger(g, fmt.Sprintf("CANARY-%d", i))
		cn.canary = true
		g.canaries = append(g.canaries, cn)
		go cn.run()
	}

	go g.sampler()
	g.startHooks()
	go g.gatewayMetricsSampler()
	go g.driver()
	go g.freezeWatch()

	prevN := 0
	var restartsDone sync.WaitGroup
	for si, s := range c.Steps {
		g.curStep.Store(int64(si))
		g.rate.Store(int64(s.Rate * 1000))
		if s.Speed == 0 {
			s.Speed = 1
		}
		g.speed.Store(int64(s.Speed * 1000))
		stepStart := time.Now()
		ramp := c.Ramp
		if si > 0 && ramp > s.Dur/3 {
			ramp = s.Dur / 3
		}
		g.events.add("step", fmt.Sprintf("%d: N=%d rate=%.0f/s dur=%s", si, s.N, s.Rate, s.Dur))
		for i := prevN; i < s.N; i++ {
			ch := g.chargers[i]
			delay := time.Duration(float64(ramp) * float64(i-prevN) / float64(s.N-prevN))
			time.AfterFunc(delay, func() {
				if g.ctx.Err() == nil {
					g.started.Add(1)
					go ch.run()
					if ch.dup {
						go ch.runShadow()
					}
				}
			})
		}
		prevN = s.N
		if si == 0 && (c.Restarts > 0 || c.Plan != "") && (g.srv != nil || g.topo != nil) {
			win := s.Dur - ramp
			plan := parsePlan(c.Plan)
			if len(plan) == 0 {
				for k := 0; k < c.Restarts; k++ {
					plan = append(plan, restartSpec{c.RestartTarget, c.RestartMode})
				}
			}
			for k := 1; k <= len(plan); k++ {
				at := ramp + win*time.Duration(k)/time.Duration(len(plan)+1)
				spec := plan[k-1]
				restartsDone.Add(1)
				time.AfterFunc(at, func() {
					defer restartsDone.Done()
					if g.ctx.Err() == nil {
						g.restart(k, spec.target, spec.mode)
					}
				})
			}
		}
		select {
		case <-time.After(time.Until(stepStart.Add(s.Dur))):
		case <-g.ctx.Done():
		}
		g.writeStepMark(si)
		if g.ctx.Err() != nil {
			break
		}
	}
	g.events.add("end", "")
	restartsDone.Wait()
	g.finish()
}

func (g *gen) writeStepMark(si int) {
	g.events.add("step_end", fmt.Sprintf("%d up=%d", si, g.up.Load()))
}

func (g *gen) waitHealth(d time.Duration) bool {
	end := time.Now().Add(d)
	for time.Now().Before(end) {
		if _, ok := g.health(2 * time.Second); ok {
			return true
		}
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

func (g *gen) health(timeout time.Duration) (time.Duration, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	t0 := time.Now()
	req, _ := http.NewRequestWithContext(ctx, "GET", "http://"+g.cfg.Addr+"/control/health", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return time.Since(t0), false
	}
	defer resp.Body.Close()
	var v struct{ OK bool }
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return time.Since(t0), v.OK
}

type restartSpec struct{ target, mode string }

func parsePlan(s string) []restartSpec {
	var out []restartSpec
	for _, it := range strings.Split(s, ",") {
		it = strings.TrimSpace(it)
		if it == "" {
			continue
		}
		t, mode, _ := strings.Cut(it, ":")
		if strings.HasPrefix(it, "pool:") { // pool:NAME:mode
			rest := strings.TrimPrefix(it, "pool:")
			n, md, _ := strings.Cut(rest, ":")
			t, mode = "pool:"+n, md
		}
		if mode == "" {
			mode = "term"
		}
		out = append(out, restartSpec{t, mode})
	}
	return out
}
