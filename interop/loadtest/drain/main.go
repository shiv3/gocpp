// Command drain compares a rolling restart of a CSMS process with and without
// Server.Drain. The fleet (N simulated 1.6J chargers) is connected to the old
// process through a small L4 balancer that sends new connections to the first
// backend whose /ready answers 200 (old, then new). The old process receives
// SIGTERM: with -mode kill it exits at once (what a plain SIGTERM does today); with
// -mode drain it is started with -drain-window and closes its sessions at a paced
// rate. The tool records how the fleet reconnects to the new process and what
// that costs it: connect attempts per second, successful upgrades per second,
// CPU, and the time to get back to 99% of the fleet.
//
//	go run ./drain -server-bin /tmp/cs-gocpp -mode drain -window 60s -out results/drain
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
)

type backend struct {
	name   string
	addr   string
	cmd    *exec.Cmd
	pid    int
	ready  atomic.Bool
	routed atomic.Int64 // connections the balancer sent here
	exited chan struct{}
}

type cfg struct {
	serverBin, mode, out, tasksetA, tasksetB string
	n                                        int
	window, hb, retryMin, retryMax, settle   time.Duration
	acceptRate                               float64
	serverArgs                               string
}

func main() {
	var c cfg
	flag.StringVar(&c.serverBin, "server-bin", "", "path to the cs-gocpp parity server")
	flag.StringVar(&c.mode, "mode", "drain", "kill (SIGTERM exits at once) | drain (SIGTERM runs Server.Drain over -window)")
	flag.StringVar(&c.out, "out", "results/drain", "output directory")
	flag.StringVar(&c.tasksetA, "taskset-a", "", "CPU list for the old server, e.g. 14,15")
	flag.StringVar(&c.tasksetB, "taskset-b", "", "CPU list for the new server, e.g. 16,17")
	flag.IntVar(&c.n, "n", 4000, "chargers")
	flag.DurationVar(&c.window, "window", 60*time.Second, "drain window (mode drain)")
	flag.DurationVar(&c.hb, "hb", 30*time.Second, "heartbeat interval")
	flag.DurationVar(&c.retryMin, "retry-min", 2*time.Second, "charger reconnect back-off start (jittered 0.5x-1.5x)")
	flag.DurationVar(&c.retryMax, "retry-max", 30*time.Second, "charger reconnect back-off cap")
	flag.DurationVar(&c.settle, "settle", 10*time.Second, "steady time before SIGTERM and after recovery")
	flag.Float64Var(&c.acceptRate, "accept-rate", 0, "new server -accept-rate (0 = off)")
	flag.StringVar(&c.serverArgs, "server-args", "", "extra args for both servers")
	flag.Parse()
	if c.serverBin == "" {
		fmt.Fprintln(os.Stderr, "-server-bin is required")
		os.Exit(2)
	}
	if err := run(c); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func startServer(c cfg, name, addr, taskset string, extra ...string) (*backend, error) {
	args := []string{"-listen", addr, "-lite", "-schema", "lenient"}
	args = append(args, extra...)
	if c.serverArgs != "" {
		args = append(args, strings.Fields(c.serverArgs)...)
	}
	bin := c.serverBin
	if taskset != "" {
		args = append([]string{"-c", taskset, c.serverBin}, args...)
		bin = "taskset"
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), "GOMAXPROCS=2")
	logf, err := os.Create(filepath.Join(c.out, name+".log"))
	if err != nil {
		return nil, err
	}
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &backend{name: name, addr: addr, cmd: cmd, pid: cmd.Process.Pid, exited: make(chan struct{})}
	go func() { _ = cmd.Wait(); close(b.exited) }()
	return b, nil
}

// readyLoop polls /ready every 100ms.
func (b *backend) readyLoop(ctx context.Context) {
	cl := &http.Client{Timeout: 500 * time.Millisecond}
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		resp, err := cl.Get("http://" + b.addr + "/ready")
		ok := err == nil && resp.StatusCode == 200
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
		b.ready.Store(ok)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// balancer sends each new connection to the first ready backend.
func balancer(ctx context.Context, ln net.Listener, bs []*backend, timeline *timeline) {
	for {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		var target *backend
		for _, b := range bs {
			if b.ready.Load() {
				target = b
				break
			}
		}
		if target == nil {
			_ = c.Close()
			continue
		}
		target.routed.Add(1)
		timeline.routed(target.name)
		go func() {
			up, err := net.DialTimeout("tcp", target.addr, 2*time.Second)
			if err != nil {
				_ = c.Close()
				return
			}
			go func() { _, _ = io.Copy(up, c); _ = up.Close() }()
			_, _ = io.Copy(c, up)
			_ = c.Close()
		}()
	}
}

// timeline holds per-second counters.
type timeline struct {
	start time.Time
	mu    sync.Mutex
	rows  map[int]*sec
}

type sec struct {
	routedOld, routedNew int
	upgrades, drops      int
	cpuOld, cpuNew       float64
	up                   int
}

func (t *timeline) at(now time.Time) *sec {
	i := int(now.Sub(t.start) / time.Second)
	r := t.rows[i]
	if r == nil {
		r = &sec{}
		t.rows[i] = r
	}
	return r
}
func (t *timeline) routed(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if name == "new" {
		t.at(time.Now()).routedNew++
	} else {
		t.at(time.Now()).routedOld++
	}
}
func (t *timeline) upgrade() { t.mu.Lock(); t.at(time.Now()).upgrades++; t.mu.Unlock() }
func (t *timeline) drop()    { t.mu.Lock(); t.at(time.Now()).drops++; t.mu.Unlock() }

func cpuTicks(pid int) (float64, bool) {
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0, false
	}
	s := string(b)
	s = s[strings.LastIndex(s, ")")+2:]
	f := strings.Fields(s)
	if len(f) < 13 {
		return 0, false
	}
	u, _ := strconv.ParseFloat(f[11], 64)
	k, _ := strconv.ParseFloat(f[12], 64)
	return (u + k) / 100, true // seconds at CLK_TCK=100
}

func charger(ctx context.Context, c cfg, id int, lb string, up *atomic.Int64, tl *timeline, jitterSeed uint64) {
	rng := rand.New(rand.NewPCG(jitterSeed, uint64(id)))
	name := fmt.Sprintf("CP%05d", id)
	attempt := 0
	for ctx.Err() == nil {
		dctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		ws, _, err := websocket.Dial(dctx, "ws://"+lb+"/ocpp/"+name, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		cancel()
		if err == nil {
			err = session(ctx, c, ws, rng, up, tl)
		}
		_ = err
		if ctx.Err() != nil {
			return
		}
		d := min(float64(c.retryMax), float64(c.retryMin)*math.Pow(2, float64(min(attempt, 10))))
		attempt++
		time.Sleep(time.Duration(d * (0.5 + rng.Float64())))
	}
}

// session returns when the socket ends. A session that stayed up resets the
// caller's back-off by being long-lived; here every successful upgrade counts.
func session(ctx context.Context, c cfg, ws *websocket.Conn, rng *rand.Rand, up *atomic.Int64, tl *timeline) error {
	defer ws.CloseNow()
	boot := []byte(`[2,"b","BootNotification",{"chargePointVendor":"v","chargePointModel":"m"}]`)
	if err := ws.Write(ctx, websocket.MessageText, boot); err != nil {
		return err
	}
	if _, _, err := ws.Read(ctx); err != nil {
		return err
	}
	up.Add(1)
	tl.upgrade()
	defer func() { up.Add(-1); tl.drop() }()
	hb := time.NewTicker(time.Duration(float64(c.hb) * (0.5 + rng.Float64())))
	defer hb.Stop()
	errc := make(chan error, 1)
	go func() {
		for {
			if _, _, err := ws.Read(ctx); err != nil {
				errc <- err
				return
			}
		}
	}()
	n := 0
	for {
		select {
		case err := <-errc:
			return err
		case <-ctx.Done():
			return ctx.Err()
		case <-hb.C:
			n++
			if err := ws.Write(ctx, websocket.MessageText, []byte(`[2,"h`+strconv.Itoa(n)+`","Heartbeat",{}]`)); err != nil {
				return err
			}
		}
	}
}

func run(c cfg) error {
	if err := os.MkdirAll(c.out, 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	oldArgs := []string{}
	if c.mode == "drain" {
		oldArgs = append(oldArgs, "-drain-window", c.window.String())
	}
	newArgs := []string{}
	if c.acceptRate > 0 {
		newArgs = append(newArgs, "-accept-rate", fmt.Sprint(c.acceptRate))
	}
	oldB, err := startServer(c, "old", "127.0.0.1:9101", c.tasksetA, oldArgs...)
	if err != nil {
		return err
	}
	newB, err := startServer(c, "new", "127.0.0.1:9102", c.tasksetB, newArgs...)
	if err != nil {
		return err
	}
	defer func() {
		for _, b := range []*backend{oldB, newB} {
			select {
			case <-b.exited:
			default:
				_ = b.cmd.Process.Kill()
				<-b.exited
			}
		}
	}()
	bs := []*backend{oldB, newB}
	for _, b := range bs {
		go b.readyLoop(ctx)
	}
	for deadline := time.Now().Add(15 * time.Second); !(oldB.ready.Load() && newB.ready.Load()); time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			return fmt.Errorf("servers not ready")
		}
	}

	ln, err := net.Listen("tcp", "127.0.0.1:9100")
	if err != nil {
		return err
	}
	defer ln.Close()
	tl := &timeline{start: time.Now(), rows: map[int]*sec{}}
	go balancer(ctx, ln, bs, tl)

	var up atomic.Int64
	seed := uint64(time.Now().UnixNano())
	var wg sync.WaitGroup
	// Ramp the fleet onto the old server at 200 connects/s.
	for i := 0; i < c.n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); charger(ctx, c, i, "127.0.0.1:9100", &up, tl, seed) }()
		if i%200 == 199 {
			time.Sleep(time.Second)
		}
	}
	for deadline := time.Now().Add(2 * time.Minute); up.Load() < int64(c.n)*99/100; time.Sleep(200 * time.Millisecond) {
		if time.Now().After(deadline) {
			return fmt.Errorf("fleet did not connect: %d/%d", up.Load(), c.n)
		}
	}
	fmt.Println("fleet up:", up.Load())
	time.Sleep(c.settle)
	pre := up.Load()
	tl.mu.Lock()
	tl.rows = map[int]*sec{}
	tl.start = time.Now()
	tl.mu.Unlock()
	routedNewBefore := newB.routed.Load()

	// Sample CPU and up every 250ms from t0.
	type sample struct {
		T              float64 `json:"t_s"`
		Up             int64   `json:"up"`
		CPUOld, CPUNew float64 `json:"-"`
	}
	var samples []sample
	var smu sync.Mutex
	sctx, scancel := context.WithCancel(ctx)
	var swg sync.WaitGroup
	swg.Add(1)
	go func() {
		defer swg.Done()
		tk := time.NewTicker(250 * time.Millisecond)
		defer tk.Stop()
		lastO, _ := cpuTicks(oldB.pid)
		lastN, _ := cpuTicks(newB.pid)
		last := time.Now()
		for {
			select {
			case <-sctx.Done():
				return
			case now := <-tk.C:
				o, okO := cpuTicks(oldB.pid)
				n, _ := cpuTicks(newB.pid)
				dt := now.Sub(last).Seconds()
				s := sample{T: now.Sub(tl.start).Seconds(), Up: up.Load(), CPUNew: (n - lastN) / dt}
				if okO {
					s.CPUOld = (o - lastO) / dt
					lastO = o
				}
				lastN, last = n, now
				smu.Lock()
				samples = append(samples, s)
				smu.Unlock()
				tl.mu.Lock()
				r := tl.at(now)
				r.cpuNew = max(r.cpuNew, s.CPUNew)
				r.cpuOld = max(r.cpuOld, s.CPUOld)
				r.up = int(s.Up)
				tl.mu.Unlock()
			}
		}
	}()

	t0 := time.Now()
	fmt.Printf("SIGTERM old (%s) with %d chargers up\n", c.mode, pre)
	_ = oldB.cmd.Process.Signal(syscall.SIGTERM)
	cpuNew0, _ := cpuTicks(newB.pid)

	limit := c.window + 150*time.Second
	var t99 float64
	for time.Since(t0) < limit {
		el := time.Since(t0).Seconds()
		if t99 == 0 && el > 1 && up.Load() >= pre*99/100 {
			t99 = el
		}
		exited := false
		select {
		case <-oldB.exited:
			exited = true
		default:
		}
		if t99 != 0 && exited && el > t99+c.settle.Seconds() {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	cpuNew1, _ := cpuTicks(newB.pid)
	scancel()
	swg.Wait()

	// Summaries.
	tl.mu.Lock()
	keys := make([]int, 0, len(tl.rows))
	for k := range tl.rows {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	var peakRouted, peakUpg, peakDrops int
	var peakCPU float64
	for _, k := range keys {
		r := tl.rows[k]
		peakRouted = max(peakRouted, r.routedNew)
		peakUpg = max(peakUpg, r.upgrades)
		peakDrops = max(peakDrops, r.drops)
		peakCPU = max(peakCPU, r.cpuNew)
	}
	csv, _ := os.Create(filepath.Join(c.out, "timeline.csv"))
	fmt.Fprintln(csv, "t_s,up,connect_attempts_new_s,upgrades_s,drops_s,cpu_new_cores,cpu_old_cores")
	for _, k := range keys {
		r := tl.rows[k]
		fmt.Fprintf(csv, "%d,%d,%d,%d,%d,%.2f,%.2f\n", k, r.up, r.routedNew, r.upgrades, r.drops, r.cpuNew, r.cpuOld)
	}
	csv.Close()
	tl.mu.Unlock()
	smu.Lock()
	minUp := pre
	var below99 float64
	for i, s := range samples {
		minUp = min(minUp, s.Up)
		if s.Up < pre*99/100 && i > 0 {
			below99 += s.T - samples[i-1].T
		}
	}
	smu.Unlock()
	sum := map[string]any{
		"mode": c.mode, "chargers": c.n, "window_s": c.window.Seconds(), "pre_up": pre,
		"min_up": minUp, "seconds_below_99pct": round(below99, 1), "t99_from_sigterm_s": round(t99, 1),
		"new_peak_connect_attempts_per_s": peakRouted, "new_peak_upgrades_per_s": peakUpg,
		"fleet_peak_drops_per_s": peakDrops, "new_peak_cpu_cores_250ms": round(peakCPU, 2),
		"new_cpu_seconds_during_event": round(cpuNew1-cpuNew0, 2),
		"new_connect_attempts_total":   newB.routed.Load() - routedNewBefore,
		"retry_min_s":                  c.retryMin.Seconds(), "retry_max_s": c.retryMax.Seconds(),
	}
	b, _ := json.MarshalIndent(sum, "", "  ")
	_ = os.WriteFile(filepath.Join(c.out, "summary.json"), b, 0o644)
	fmt.Println(string(b))
	cancel()
	wg.Wait()
	return nil
}

func round(v float64, d int) float64 { p := math.Pow(10, float64(d)); return math.Round(v*p) / p }
