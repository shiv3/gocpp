package main

// Capacity-test extensions: multiple loopback source addresses (more than ~28k
// connections from one source IP exhaust the ephemeral port range), cgroup
// sampling of the server process, runtime info and online stop criteria.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type capConfig struct {
	SrcAddrs   int           // charger source addresses 127.0.0.2.. (0 = default source)
	StopHB     time.Duration // abort when the trailing 30 s Heartbeat p99 exceeds this (0 = off)
	MarkHB     []time.Duration
	StopConn   float64 // abort when more than this fraction of connect attempts fail (30 s window)
	StopMemPct float64 // abort when the server cgroup memory exceeds this percentage of memory.max
	GenMemMB   int
	RampFull   bool
}

var capCfg capConfig

type capState struct {
	mu                sync.Mutex
	attempts          atomic.Int64
	prevAtt, prevErr  int64
	prevT             time.Time
	cgPath            string
	cgPid             int
	prevUsec, prevThr int64
	cgf               *os.File
	marked            map[time.Duration]bool
	attHist           [][2]int64 // trailing (attempts, errors) per sample
}

var cap_ = &capState{marked: map[time.Duration]bool{}}

// dialer spreads outgoing connections over 127.0.0.2 .. 127.0.0.(N+1).
func capDialContext() func(ctx context.Context, network, addr string) (net.Conn, error) {
	n := capCfg.SrcAddrs
	var rr atomic.Uint64
	d := &net.Dialer{Timeout: 30 * time.Second}
	if n <= 0 {
		return d.DialContext
	}
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		i := int(rr.Add(1) % uint64(n))
		dd := *d
		dd.LocalAddr = &net.TCPAddr{IP: net.IPv4(127, 0, byte((i+2)/256%256), byte((i+2)%256+0))}
		return dd.DialContext(ctx, network, addr)
	}
}

func cgroupOf(pid int) string {
	f, err := os.Open(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "0::") {
			return "/sys/fs/cgroup" + strings.TrimPrefix(sc.Text(), "0::")
		}
	}
	return ""
}

func readInt(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	s := strings.TrimSpace(string(b))
	if s == "max" {
		return -2
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

func readKV(path string) map[string]int64 {
	out := map[string]int64{}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) == 2 {
			v, _ := strconv.ParseInt(f[1], 10, 64)
			out[f[0]] = v
		}
	}
	return out
}

// capSample runs once per sampler tick: cgroup stats and stop criteria.
func (g *gen) capSample(now time.Time) {
	cs := cap_
	cs.mu.Lock()
	defer cs.mu.Unlock()
	if cs.cgf == nil {
		cs.cgf, _ = os.Create(filepath.Join(g.cfg.Out, "cgroup.csv"))
		fmt.Fprintln(cs.cgf, "t_s,mem_cur_mb,mem_max_mb,mem_peak_mb,anon_mb,cpu_cores,throttled_pct,oom_kill,gomaxprocs,gomemlimit_mb")
	}
	pid := 0
	if g.srv != nil {
		pid = int(g.srv.pid.Load())
	}
	if pid != 0 && pid != cs.cgPid {
		cs.cgPid, cs.cgPath, cs.prevUsec, cs.prevThr = pid, cgroupOf(pid), 0, 0
		g.events.add("cgroup", cs.cgPath)
	}
	if cs.cgPath != "" {
		cur := readInt(cs.cgPath + "/memory.current")
		mx := readInt(cs.cgPath + "/memory.max")
		peak := readInt(cs.cgPath + "/memory.peak")
		st := readKV(cs.cgPath + "/memory.stat")
		cpu := readKV(cs.cgPath + "/cpu.stat")
		ev := readKV(cs.cgPath + "/memory.events")
		cores, thr := 0.0, 0.0
		if cs.prevUsec > 0 && !cs.prevT.IsZero() {
			dt := now.Sub(cs.prevT).Seconds()
			cores = float64(cpu["usage_usec"]-cs.prevUsec) / 1e6 / dt
			thr = float64(cpu["throttled_usec"]-cs.prevThr) / 1e6 / dt * 100
		}
		cs.prevUsec, cs.prevThr, cs.prevT = cpu["usage_usec"], cpu["throttled_usec"], now
		gmp, gml := -1.0, -1.0
		if rt := g.srvRuntimeInfo(); rt != nil {
			gmp, gml = rt[0], rt[1]
		}
		fmt.Fprintf(cs.cgf, "%.0f,%.1f,%.0f,%.1f,%.1f,%.2f,%.1f,%d,%.0f,%.0f\n", g.since(), float64(cur)/1048576, float64(mx)/1048576,
			float64(peak)/1048576, float64(st["anon"])/1048576, cores, thr, ev["oom_kill"], gmp, gml)
		if mx > 0 && cur > 0 && capCfg.StopMemPct > 0 && float64(cur) > capCfg.StopMemPct/100*float64(mx) {
			g.abort(fmt.Sprintf("criterion: server cgroup memory %.0f MB > %.0f%% of %.0f MB", float64(cur)/1048576, capCfg.StopMemPct, float64(mx)/1048576))
		}
	}
	// connect error rate over the trailing 30 s
	att := cs.attempts.Load()
	g.met.mu.Lock()
	errs := 0
	for _, v := range g.met.connErrs {
		errs += v
	}
	// heartbeat p99 over the trailing three windows
	var h hist
	cur := int(time.Since(g.start) / winLen)
	for i := cur - 3; i < cur; i++ {
		if w := g.met.wins[i]; w != nil && w.req["Heartbeat"] != nil {
			h.merge(w.req["Heartbeat"])
		}
	}
	g.met.mu.Unlock()
	cs.attHist = append(cs.attHist, [2]int64{att, int64(errs)})
	if len(cs.attHist) > 6 {
		cs.attHist = cs.attHist[1:]
	}
	if len(cs.attHist) >= 6 {
		da, de := att-cs.attHist[0][0], int64(errs)-cs.attHist[0][1]
		if da >= 200 && capCfg.StopConn > 0 && float64(de)/float64(da) > capCfg.StopConn {
			g.abort(fmt.Sprintf("criterion: connect errors %d/%d (%.1f%%) in 30 s", de, da, 100*float64(de)/float64(da)))
		}
	}
	if h.n >= 30 {
		p99 := h.pct(0.99)
		for _, t := range capCfg.MarkHB {
			if p99 > t && !cs.marked[t] {
				cs.marked[t] = true
				g.events.add("crit_hb_p99", fmt.Sprintf(">%s: p99=%s n=%d up=%d step=%d", t, p99, h.n, g.up.Load(), g.curStep.Load()))
			}
		}
		if capCfg.StopHB > 0 && p99 > capCfg.StopHB {
			g.abort(fmt.Sprintf("criterion: Heartbeat p99 %s > %s (up=%d)", p99, capCfg.StopHB, g.up.Load()))
		}
	}
}

// srvRuntimeInfo returns GOMAXPROCS and the memory limit (MB) the server reports.
func (g *gen) srvRuntimeInfo() []float64 {
	cl := &http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + g.cfg.Addr + "/control/runtime")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var v struct {
		GOMAXPROCS float64 `json:"gomaxprocs"`
		GOMEMLIMIT float64 `json:"gomemlimit"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil {
		return nil
	}
	ml := v.GOMEMLIMIT / 1048576
	if v.GOMEMLIMIT > 1e18 {
		ml = 0 // no limit
	}
	return []float64{v.GOMAXPROCS, ml}
}
