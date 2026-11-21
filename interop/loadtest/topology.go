package main

import (
	"bufio"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/alicebob/miniredis/v2"
)

// A topology is the split deployment the harness drives instead of one server:
// G gateways (cmd/gocpp-gateway), backends in named pools (cs-gocpp -mode
// backend) and a Redis-compatible registry. Chargers connect to the gateways
// (a random one per connect, like a load balancer); the first backend also
// serves the /control API at -addr.
type topology struct {
	g     *gen
	redis *miniredis.Miniredis
	gws   []*proc
	bes   []*proc
	rr    atomic.Int64
}

// proc is one supervised process of the topology.
type proc struct {
	name, role, pool string
	args             []string
	taskset          string
	ws, link, admin  string // gateways
	ctl              string // control address (first backend)
	mu               sync.Mutex
	cmd              *exec.Cmd
	pid              atomic.Int64
	exited           chan struct{}
	wanted           atomic.Bool
	prevTicks        uint64
	prevT            time.Time
}

type poolSpec struct {
	name  string
	count int
}

func parsePools(s string) ([]poolSpec, error) {
	var out []poolSpec
	for _, p := range strings.Split(s, ",") {
		f := strings.Split(strings.TrimSpace(p), ":")
		if len(f) != 2 {
			return nil, fmt.Errorf("bad pool %q (want name:count)", p)
		}
		n, err := strconv.Atoi(f[1])
		if err != nil {
			return nil, err
		}
		out = append(out, poolSpec{f[0], n})
	}
	return out, nil
}

func hostPort(base string, off int) string {
	h, p, _ := net.SplitHostPort(base)
	n, _ := strconv.Atoi(p)
	return net.JoinHostPort(h, strconv.Itoa(n+off))
}

func newTopology(g *gen) (*topology, error) {
	c := g.cfg
	t := &topology{g: g}
	var err error
	t.redis, err = miniredis.Run()
	if err != nil {
		return nil, err
	}
	pools, err := parsePools(c.BackendPools)
	if err != nil {
		return nil, err
	}
	for i := 0; i < c.Gateways; i++ {
		p := &proc{name: fmt.Sprintf("gw-%d", i), role: "gateway", taskset: c.GwTaskset,
			ws: hostPort(c.Addr, 100+i), link: hostPort(c.Addr, 200+i), admin: hostPort(c.Addr, 300+i)}
		p.args = []string{"serve", "--id", p.name, "--listen", p.ws, "--link-listen", p.link, "--admin", p.admin,
			"--subprotocols", "ocpp1.6", "--redis-addr", t.redis.Addr(), "--accept-rate", "0",
			"--drain-window", "20s", "--call-timeout", c.CallTimeout.String(), "--handler-timeout", "25s", "--log-level", "warn"}
		p.args = append(p.args, strings.Fields(c.GwArgs)...)
		t.gws = append(t.gws, p)
	}
	var links []string
	for _, gw := range t.gws {
		links = append(links, gw.link)
	}
	j := 0
	for _, ps := range pools {
		for k := 0; k < ps.count; k++ {
			p := &proc{name: fmt.Sprintf("be-%s-%d", ps.name, k), role: "backend", pool: ps.name, taskset: c.BeTaskset}
			pool := ps.name
			if pool == "default" {
				pool = ""
			}
			listen := hostPort(c.Addr, 400+j)
			if j == 0 {
				listen = c.Addr
				p.ctl = c.Addr
			}
			p.args = []string{"-mode", "backend", "-listen", listen, "-lite", "-call-timeout", c.CallTimeout.String(),
				"-gateways", strings.Join(links, ","), "-redis", t.redis.Addr(), "-pool", pool, "-id", p.name,
				"-schema", "off"}
			p.args = append(p.args, strings.Fields(c.ServerArgs)...)
			t.bes = append(t.bes, p)
			j++
		}
	}
	return t, nil
}

func (t *topology) all() []*proc { return append(append([]*proc{}, t.gws...), t.bes...) }

func (t *topology) bin(p *proc) string {
	if p.role == "gateway" {
		return t.g.cfg.GatewayBin
	}
	return t.g.cfg.ServerBin
}

func (t *topology) startProc(p *proc) error {
	c := t.g.cfg
	args := p.args
	bin := t.bin(p)
	if p.taskset != "" {
		args = append([]string{"-c", p.taskset, bin}, args...)
		bin = "taskset"
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOMAXPROCS=%d", c.ServerProcs))
	lf, err := os.OpenFile(filepath.Join(c.Out, p.name+".log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	cmd.Stdout, cmd.Stderr = lf, lf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return err
	}
	ex := make(chan struct{})
	p.mu.Lock()
	p.cmd, p.exited = cmd, ex
	p.pid.Store(int64(cmd.Process.Pid))
	p.prevTicks = 0
	p.mu.Unlock()
	p.wanted.Store(false)
	go func() {
		_ = cmd.Wait()
		close(ex)
		_ = lf.Close()
		if !p.wanted.Load() {
			t.g.events.add("proc_crash", p.name)
		}
	}()
	return nil
}

func (t *topology) stopProc(p *proc, sig syscall.Signal) time.Duration {
	p.mu.Lock()
	cmd, ex := p.cmd, p.exited
	p.mu.Unlock()
	if cmd == nil {
		return 0
	}
	p.wanted.Store(true)
	t0 := time.Now()
	_ = syscall.Kill(-cmd.Process.Pid, sig)
	select {
	case <-ex:
	case <-time.After(60 * time.Second):
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-ex
	}
	return time.Since(t0)
}

func (p *proc) ready(t *topology) bool {
	switch p.role {
	case "gateway":
		resp, err := http.Get("http://" + p.admin + "/healthz")
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == 200
	default:
		if p.ctl != "" {
			_, ok := t.g.health(time.Second)
			return ok
		}
		return p.pid.Load() != 0
	}
}

// startAll starts gateways, then backends, and waits until the control API
// answers and every gateway reports a worker attached (/readyz).
func (t *topology) startAll() error {
	for _, p := range t.gws {
		if err := t.startProc(p); err != nil {
			return err
		}
	}
	for _, p := range t.bes {
		if err := t.startProc(p); err != nil {
			return err
		}
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		ok := true
		for _, gw := range t.gws {
			resp, err := http.Get("http://" + gw.admin + "/readyz")
			if err != nil {
				ok = false
				break
			}
			_ = resp.Body.Close()
			if resp.StatusCode != 200 {
				ok = false
			}
		}
		if ok {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("topology not ready after 60s")
}

func (t *topology) stopAll() {
	for _, p := range t.all() {
		go t.stopProc(p, syscall.SIGTERM)
	}
	time.Sleep(2 * time.Second)
	for _, p := range t.all() {
		t.stopProc(p, syscall.SIGTERM)
	}
	if t.redis != nil {
		t.redis.Close()
	}
}

// pickWS returns the WebSocket address of a random gateway.
func (t *topology) pickWS() string { return t.gws[rand.Intn(len(t.gws))].ws }

// target selects the processes a restart acts on, round-robin over the
// candidates: gateway | backend | pool:NAME | server (the control backend).
func (t *topology) target() *proc {
	var c []*proc
	switch ts := t.g.cfg.RestartTarget; {
	case ts == "gateway":
		c = t.gws
	case strings.HasPrefix(ts, "pool:"):
		for _, p := range t.bes {
			if p.pool == strings.TrimPrefix(ts, "pool:") && p.ctl == "" {
				c = append(c, p)
			}
		}
	default: // backend: any backend except the control one, which the harness talks to
		for _, p := range t.bes {
			if p.ctl == "" {
				c = append(c, p)
			}
		}
	}
	if len(c) == 0 {
		c = t.bes
	}
	return c[int(t.rr.Add(1)-1)%len(c)]
}

// gatewayConnections sums the sessions the gateways report.
func (t *topology) gatewayConnections() int {
	total := 0
	for _, gw := range t.gws {
		resp, err := http.Get("http://" + gw.admin + "/metrics")
		if err != nil {
			return -1
		}
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			l := sc.Text()
			if strings.HasPrefix(l, "gocpp_connections{") {
				if f := strings.Fields(l); len(f) == 2 {
					v, _ := strconv.ParseFloat(f[1], 64)
					total += int(v)
				}
			}
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
	return total
}

// sample appends one line per process to procs.csv.
func (t *topology) sample(w io.Writer, now time.Time, secs float64) {
	for _, p := range t.all() {
		pid := int(p.pid.Load())
		if pid == 0 {
			continue
		}
		rss, ticks, thr, fds := readProc(pid)
		cores := 0.0
		if p.prevTicks != 0 && ticks >= p.prevTicks {
			cores = float64(ticks-p.prevTicks) / 100 / now.Sub(p.prevT).Seconds()
		}
		p.prevTicks, p.prevT = ticks, now
		fmt.Fprintf(w, "%.0f,%s,%s,%s,%d,%.1f,%.2f,%d,%d\n", secs, p.name, p.role, p.pool, pid, rss, cores, thr, fds)
	}
}
