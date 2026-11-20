package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const winLen = 10 * time.Second

type window struct {
	req     map[string]*hist
	reqErr  map[string]int
	calls   map[string]int
	callLat map[string]*hist
	restart bool
	step    int
}

type freezeRec struct {
	AtS       float64 `json:"at_s"`
	Why       string  `json:"why"`
	Recovered bool    `json:"recovered"`
	LastedS   float64 `json:"lasted_s"`
	Killed    bool    `json:"killed_by_harness"`
}

type metrics struct {
	g             *gen
	mu            sync.Mutex
	wins          map[int]*window
	restarts      []restartRec
	freezes       []freezeRec
	restartActive atomic.Bool
	connErrs      map[string]int
	reqErrTotal   map[string]int
	csmsSeen      [2]int64 // [unmuted, muted] CSMS calls delivered to chargers
	inflight      atomic.Int64
	tl            *os.File
	prevMsgs      int64
	prevCPU       float64
	prevT         time.Time
	prevTicks     uint64
	prevGenTicks  uint64
	prevRT        *runtimeSnap
}

func newMetrics(g *gen) *metrics {
	m := &metrics{g: g, wins: map[int]*window{}, connErrs: map[string]int{}, reqErrTotal: map[string]int{}}
	f, _ := os.Create(filepath.Join(g.cfg.Out, "timeline.csv"))
	m.tl = f
	fmt.Fprintln(f, "t_s,step,target_started,chargers_up,server_connected,health_ms,server_pid,srv_rss_mb,srv_cpu_cores,srv_threads,srv_fds,srv_goroutines,gen_rss_mb,gen_goroutines,mem_avail_mb,canary_max_rtt_ms,canary_up,calls_inflight,msgs_per_s,restart_active,gen_cpu_cores,alloc_mb_s,alloc_kb_req,gc_cycles_s,gc_cpu_cores,gc_assist_cores,sched_p99_ms,heap_obj_mb,stacks_mb")
	return m
}

func (m *metrics) win() *window {
	i := int(time.Since(m.g.start) / winLen)
	w := m.wins[i]
	if w == nil {
		w = &window{req: map[string]*hist{}, reqErr: map[string]int{}, calls: map[string]int{}, callLat: map[string]*hist{}, step: int(m.g.curStep.Load())}
		m.wins[i] = w
	}
	if m.restartActive.Load() {
		w.restart = true
	}
	return w
}

func (m *metrics) req(action string, d time.Duration) {
	m.mu.Lock()
	w := m.win()
	h := w.req[action]
	if h == nil {
		h = &hist{}
		w.req[action] = h
	}
	h.add(d)
	m.mu.Unlock()
}

func (m *metrics) reqErr(action, kind string) {
	m.mu.Lock()
	m.win().reqErr[action+"/"+kind]++
	m.reqErrTotal[action+"/"+kind]++
	m.mu.Unlock()
}

func (m *metrics) connErr(kind string) {
	m.mu.Lock()
	m.connErrs[kind]++
	m.mu.Unlock()
}

func (m *metrics) csmsCallSeen(muted bool) {
	i := 0
	if muted {
		i = 1
	}
	atomic.AddInt64(&m.csmsSeen[i], 1)
}

func (m *metrics) callOutcome(kind string, d time.Duration) {
	m.mu.Lock()
	w := m.win()
	w.calls[kind]++
	h := w.callLat[kind]
	if h == nil {
		h = &hist{}
		w.callLat[kind] = h
	}
	h.add(d)
	m.mu.Unlock()
}

func (m *metrics) addRestart(r restartRec) {
	m.mu.Lock()
	m.restarts = append(m.restarts, r)
	m.mu.Unlock()
}
func (m *metrics) addFreeze(at float64, why string) {
	m.mu.Lock()
	m.freezes = append(m.freezes, freezeRec{AtS: at, Why: why})
	m.mu.Unlock()
}
func (m *metrics) freezeCount() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.freezes) }
func (m *metrics) freezeRecovered(d float64) {
	m.mu.Lock()
	f := &m.freezes[len(m.freezes)-1]
	f.Recovered, f.LastedS = true, d
	m.mu.Unlock()
}
func (m *metrics) markFreezeUnrecovered() {
	m.mu.Lock()
	m.freezes[len(m.freezes)-1].Killed = true
	m.mu.Unlock()
}

func classifyErr(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "refused"):
		return "refused"
	case strings.Contains(s, "deadline") || strings.Contains(s, "timeout"):
		return "timeout"
	case strings.Contains(s, "reset"):
		return "reset"
	case strings.Contains(s, "EOF"):
		return "eof"
	case strings.Contains(s, "too many open"):
		return "emfile"
	case strings.Contains(s, "503") || strings.Contains(s, "bad status"):
		return "bad_status"
	}
	return "other"
}

// ---------------------------------------------------------------- /proc

func readProc(pid int) (rssMB float64, ticks uint64, threads int, fds int) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return
	}
	s := string(b)
	i := strings.LastIndexByte(s, ')')
	f := strings.Fields(s[i+2:])
	if len(f) > 17 {
		u, _ := strconv.ParseUint(f[11], 10, 64)
		k, _ := strconv.ParseUint(f[12], 10, 64)
		ticks = u + k
		threads, _ = strconv.Atoi(f[17])
		rp, _ := strconv.ParseFloat(f[21], 64)
		rssMB = rp * float64(os.Getpagesize()) / 1048576
	}
	if d, err := os.ReadDir(fmt.Sprintf("/proc/%d/fd", pid)); err == nil {
		fds = len(d)
	}
	return
}

func memAvailMB() float64 {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return -1
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "MemAvailable:") {
			v, _ := strconv.ParseFloat(strings.Fields(sc.Text())[1], 64)
			return v / 1024
		}
	}
	return -1
}

func (g *gen) srvGoroutines() int {
	if g.cfg.Pprof == "" {
		return -1
	}
	cl := &http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + g.cfg.Pprof + "/debug/pprof/goroutine?debug=1")
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	line, _ := bufio.NewReader(io.LimitReader(resp.Body, 200)).ReadString('\n')
	// "goroutine profile: total 12345"
	f := strings.Fields(line)
	if len(f) >= 4 {
		n, _ := strconv.Atoi(f[3])
		return n
	}
	return -1
}

func (g *gen) srvConnected() int {
	cl := &http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + g.cfg.Addr + "/control/count")
	if err != nil {
		return -1
	}
	defer resp.Body.Close()
	var v struct{ Connected int }
	_ = json.NewDecoder(resp.Body).Decode(&v)
	return v.Connected
}

func (g *gen) saveHeap(name string) {
	if g.cfg.Pprof == "" {
		return
	}
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Get("http://" + g.cfg.Pprof + "/debug/pprof/heap")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	f, _ := os.Create(filepath.Join(g.cfg.Out, "dumps", name+".pb.gz"))
	defer f.Close()
	_, _ = io.Copy(f, resp.Body)
}

func (g *gen) sampler() {
	m := g.met
	t := time.NewTicker(g.cfg.Sample)
	defer t.Stop()
	lastHeap := time.Now()
	m.prevT = time.Now()
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-t.C:
		}
		now := time.Now()
		var rss, cores float64
		var thr, fds, pid int
		if g.srv != nil {
			pid = int(g.srv.pid.Load())
			var ticks uint64
			rss, ticks, thr, fds = readProc(pid)
			if m.prevTicks != 0 && ticks >= m.prevTicks {
				cores = float64(ticks-m.prevTicks) / 100 / now.Sub(m.prevT).Seconds()
			}
			m.prevTicks = ticks
		}
		m.prevT = now
		hd, ok := g.health(4 * time.Second)
		hms := float64(hd.Microseconds()) / 1000
		if !ok {
			hms = -1
		}
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		genRSS, gticks, _, _ := readProc(os.Getpid())
		genCores := 0.0
		if m.prevGenTicks != 0 && gticks >= m.prevGenTicks {
			genCores = float64(gticks-m.prevGenTicks) / 100 / g.cfg.Sample.Seconds()
		}
		m.prevGenTicks = gticks
		avail := memAvailMB()
		var cmax int64
		cup := 0
		for _, cn := range g.canaries {
			if v := cn.maxRTT.Swap(0); v > cmax {
				cmax = v
			}
			if cn.canaryUp.Load() {
				cup++
			}
		}
		msgs := g.msgs.Load()
		mps := float64(msgs-m.prevMsgs) / g.cfg.Sample.Seconds()
		m.prevMsgs = msgs
		sgo := g.srvGoroutines()
		rt := m.runtimeDelta(g, now, msgs)
		fmt.Fprintf(m.tl, "%.0f,%d,%d,%d,%d,%.1f,%d,%.1f,%.2f,%d,%d,%d,%.1f,%d,%.0f,%.1f,%d,%d,%.0f,%v,%.2f,%s\n",
			g.since(), g.curStep.Load(), g.started.Load(), g.up.Load(), g.srvConnected(), hms, pid, rss, cores, thr, fds, sgo,
			genRSS, runtime.NumGoroutine(), avail, float64(cmax)/1e6, cup, m.inflight.Load(), mps, m.restartActive.Load(), genCores, rt)
		if avail >= 0 && avail < float64(g.cfg.MinAvailMB) {
			g.abort(fmt.Sprintf("system MemAvailable %.0f MB below %d MB", avail, g.cfg.MinAvailMB))
		}
		if time.Since(lastHeap) > 5*time.Minute {
			lastHeap = time.Now()
			go g.saveHeap(fmt.Sprintf("heap-%04.0fs", g.since()))
		}
	}
}

// ---------------------------------------------------------------- CSMS call driver

func (g *gen) driver() {
	actions := []struct {
		name, payload string
		w             int
	}{
		{"GetConfiguration", `{}`, 5},
		{"TriggerMessage", `{"requestedMessage":"Heartbeat"}`, 3},
		{"RemoteStartTransaction", `{"idTag":"REMOTE1","connectorId":1}`, 2},
	}
	rng := rand.New(rand.NewSource(7))
	var hot []string
	for _, ch := range g.chargers {
		if len(hot) >= g.cfg.Hot {
			break
		}
		if ch.mute {
			hot = append(hot, ch.id)
		}
	}
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	var tokens float64
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-tick.C:
		}
		tokens += float64(g.rate.Load()) / 1000 * 0.01
		for tokens >= 1 {
			tokens--
			n := int(g.started.Load())
			if n == 0 {
				continue
			}
			if g.met.inflight.Load() > 6000 {
				g.met.callOutcome("driver_saturated", 0)
				continue
			}
			id := g.chargers[rng.Intn(min(n, len(g.chargers)))].id
			if len(hot) > 0 && rng.Float64() < g.cfg.HotShare {
				id = hot[rng.Intn(len(hot))]
			}
			r := rng.Intn(10)
			a := actions[0]
			for _, x := range actions {
				if r < x.w {
					a = x
					break
				}
				r -= x.w
			}
			g.met.inflight.Add(1)
			go g.doCall(id, a.name, a.payload)
		}
	}
}

func (g *gen) doCall(id, action, payload string) {
	defer g.met.inflight.Add(-1)
	body, _ := json.Marshal(map[string]any{"cpId": id, "action": action, "payload": json.RawMessage(payload)})
	t0 := time.Now()
	ctx, cancel := context.WithTimeout(g.ctx, 90*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+g.cfg.Addr+"/control/call", bytes.NewReader(body))
	resp, err := g.ctlc.Do(req)
	kind := "ok"
	if err != nil {
		if g.ctx.Err() != nil {
			return
		}
		kind = "http_" + classifyErr(err)
	} else {
		var v struct {
			OK        bool
			Kind      string
			CallError *struct{ Code string }
		}
		_ = json.NewDecoder(resp.Body).Decode(&v)
		resp.Body.Close()
		switch {
		case v.OK:
		case v.CallError != nil:
			kind = "callerror"
		case v.Kind != "":
			kind = v.Kind
		default:
			kind = "other"
		}
	}
	g.met.callOutcome(kind, time.Since(t0))
}

// ---------------------------------------------------------------- summary

type pct struct {
	N                        int64 `json:"n"`
	P50, P95, P99, Max, Mean float64
}

func toPct(h *hist) pct {
	return pct{N: h.n, P50: ms(h.pct(0.5)), P95: ms(h.pct(0.95)), P99: ms(h.pct(0.99)), Max: ms(h.max)}
}
func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func (g *gen) finish() {
	m := g.met
	m.mu.Lock()
	defer m.mu.Unlock()
	type agg struct{ steady, restart, all map[string]*hist }
	mk := func() map[string]*hist { return map[string]*hist{} }
	a := agg{mk(), mk(), mk()}
	perStep := map[int]map[string]*hist{}
	calls := map[string]int{}
	callLat := mk()
	errs := map[string]int{}
	var keys []int
	for k := range m.wins {
		keys = append(keys, k)
	}
	sort.Ints(keys)
	lf, _ := os.Create(filepath.Join(g.cfg.Out, "latency.csv"))
	fmt.Fprintln(lf, "window_start_s,step,restart,action,n,p50_ms,p95_ms,p99_ms,max_ms")
	cf, _ := os.Create(filepath.Join(g.cfg.Out, "calls.csv"))
	fmt.Fprintln(cf, "window_start_s,outcome,n,p50_ms,p99_ms")
	add := func(dst map[string]*hist, k string, h *hist) {
		if dst[k] == nil {
			dst[k] = &hist{}
		}
		dst[k].merge(h)
	}
	for _, k := range keys {
		w := m.wins[k]
		for act, h := range w.req {
			add(a.all, act, h)
			if w.restart {
				add(a.restart, act, h)
			} else {
				add(a.steady, act, h)
				if perStep[w.step] == nil {
					perStep[w.step] = mk()
				}
				add(perStep[w.step], act, h)
			}
			fmt.Fprintf(lf, "%d,%d,%v,%s,%d,%.1f,%.1f,%.1f,%.1f\n", k*10, w.step, w.restart, act, h.n, ms(h.pct(.5)), ms(h.pct(.95)), ms(h.pct(.99)), ms(h.max))
		}
		for e, n := range w.reqErr {
			errs[e] += n
		}
		for o, n := range w.calls {
			calls[o] += n
			add(callLat, o, w.callLat[o])
			fmt.Fprintf(cf, "%d,%s,%d,%.1f,%.1f\n", k*10, o, n, ms(w.callLat[o].pct(.5)), ms(w.callLat[o].pct(.99)))
		}
	}
	lf.Close()
	cf.Close()
	conv := func(mm map[string]*hist) map[string]pct {
		o := map[string]pct{}
		for k, h := range mm {
			o[k] = toPct(h)
		}
		return o
	}
	steps := map[string]map[string]pct{}
	for si, mm := range perStep {
		steps[strconv.Itoa(si)] = conv(mm)
	}
	why, _ := g.abortWhy.Load().(string)
	sum := map[string]any{
		"label": g.cfg.Label, "config": g.cfg, "duration_s": g.since(),
		"latency_steady_ms": conv(a.steady), "latency_restart_windows_ms": conv(a.restart), "latency_all_ms": conv(a.all),
		"latency_by_step_steady_ms": steps,
		"calls":                     calls, "call_latency_ms": conv(callLat),
		"request_errors": errs, "connect_errors": m.connErrs,
		"csms_calls_delivered": map[string]int64{"unmuted": atomic.LoadInt64(&m.csmsSeen[0]), "muted_dropped": atomic.LoadInt64(&m.csmsSeen[1])},
		"restarts":             m.restarts, "freezes": m.freezes, "server_crashes": 0,
		"aborted": g.aborted.Load(), "abort_reason": why, "final_up": g.up.Load(),
	}
	if g.srv != nil {
		sum["server_crashes"] = g.srv.crashes.Load()
	}
	b, _ := json.MarshalIndent(sum, "", " ")
	_ = os.WriteFile(filepath.Join(g.cfg.Out, "summary.json"), b, 0o644)
	g.events.add("finish", "summary written")
	if g.srv != nil {
		g.cancel()
		g.srv.stop(15) // SIGTERM
	}
	m.tl.Close()
}

// ---------------------------------------------------------------- server runtime metrics

// runtimeSnap is one /control/runtime reading.
type runtimeSnap struct {
	t                            time.Time
	msgs                         int64
	alloc, cycles, assist, gcCPU float64
	heapObj, stacks              float64
	schedBuckets                 []float64
	schedCounts                  []uint64
}

func (g *gen) srvRuntime() *runtimeSnap {
	cl := &http.Client{Timeout: 3 * time.Second}
	resp, err := cl.Get("http://" + g.cfg.Addr + "/control/runtime")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var v struct {
		Alloc        float64   `json:"alloc_bytes"`
		Cycles       float64   `json:"gc_cycles"`
		Assist       float64   `json:"gc_assist_cpu_s"`
		GCCPU        float64   `json:"gc_total_cpu_s"`
		HeapObj      float64   `json:"heap_objects_bytes"`
		Stacks       float64   `json:"heap_stacks_bytes"`
		SchedBuckets []float64 `json:"sched_buckets"`
		SchedCounts  []uint64  `json:"sched_counts"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil || v.Alloc == 0 {
		return nil // older server without the endpoint, or an error body
	}
	return &runtimeSnap{t: time.Now(), alloc: v.Alloc, cycles: v.Cycles, assist: v.Assist, gcCPU: v.GCCPU,
		heapObj: v.HeapObj, stacks: v.Stacks, schedBuckets: v.SchedBuckets, schedCounts: v.SchedCounts}
}

// schedP99 returns the p99 (bucket upper bound, ms) of the scheduler latencies
// observed between two cumulative histograms; -1 when there are none.
func schedP99(prev, cur *runtimeSnap) float64 {
	if len(cur.schedCounts) == 0 || len(cur.schedCounts) != len(prev.schedCounts) {
		return -1
	}
	var total uint64
	d := make([]uint64, len(cur.schedCounts))
	for i := range d {
		if cur.schedCounts[i] >= prev.schedCounts[i] {
			d[i] = cur.schedCounts[i] - prev.schedCounts[i]
		}
		total += d[i]
	}
	if total == 0 {
		return -1
	}
	want := uint64(float64(total)*0.99 + 0.999)
	var acc uint64
	for i, n := range d {
		acc += n
		if acc >= want {
			return cur.schedBuckets[i+1] * 1000
		}
	}
	return -1
}

// runtimeDelta samples the server and returns the new CSV columns
// (alloc_mb_s,alloc_kb_req,gc_cycles_s,gc_cpu_cores,gc_assist_cores,sched_p99_ms,
// heap_obj_mb,stacks_mb); -1 everywhere when unavailable (first sample, server
// restarted, no endpoint).
func (m *metrics) runtimeDelta(g *gen, now time.Time, msgs int64) string {
	const none = "-1,-1,-1,-1,-1,-1,-1,-1"
	cur := g.srvRuntime()
	prev := m.prevRT
	m.prevRT = nil
	if cur == nil {
		return none
	}
	cur.msgs = msgs
	m.prevRT = cur
	if prev == nil || cur.alloc < prev.alloc { // restarted server: counters reset
		return none
	}
	dt := cur.t.Sub(prev.t).Seconds()
	dm := float64(cur.msgs - prev.msgs)
	kbReq := -1.0
	if dm > 0 {
		kbReq = (cur.alloc - prev.alloc) / 1024 / dm
	}
	return fmt.Sprintf("%.1f,%.2f,%.2f,%.3f,%.3f,%.3f,%.1f,%.1f",
		(cur.alloc-prev.alloc)/1048576/dt, kbReq, (cur.cycles-prev.cycles)/dt,
		(cur.gcCPU-prev.gcCPU)/dt, (cur.assist-prev.assist)/dt, schedP99(prev, cur),
		cur.heapObj/1048576, cur.stacks/1048576)
}
