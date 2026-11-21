package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

type eventLog struct {
	mu sync.Mutex
	f  *os.File
	t0 time.Time
}

func newEventLog(path string) *eventLog {
	f, _ := os.Create(path)
	return &eventLog{f: f, t0: time.Now()}
}

func (e *eventLog) add(kind, msg string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	line := fmt.Sprintf("%s\t%.1f\t%s\t%s\n", time.Now().UTC().Format("15:04:05.000"), time.Since(e.t0).Seconds(), kind, msg)
	fmt.Fprint(e.f, line)
	fmt.Print(line)
}

type server struct {
	g       *gen
	mu      sync.Mutex
	cmd     *exec.Cmd
	pid     atomic.Int64
	exited  chan struct{}
	wanted  atomic.Bool // an exit is expected
	logf    *os.File
	crashes atomic.Int64
}

func newServer(g *gen) *server {
	f, err := os.OpenFile(filepath.Join(g.cfg.Out, "server.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		panic(err)
	}
	return &server{g: g, logf: f}
}

func (s *server) start() error {
	c := s.g.cfg
	args := []string{}
	bin := c.ServerBin
	if c.Taskset != "" {
		args = append(args, "-c", c.Taskset, c.ServerBin)
		bin = "taskset"
	}
	args = append(args, "-listen", c.Addr, "-lite", "-call-timeout", c.CallTimeout.String())
	if c.Pprof != "" {
		args = append(args, "-pprof", c.Pprof)
	}
	if c.ServerArgs != "" {
		args = append(args, strings.Fields(c.ServerArgs)...)
	}
	cmd := exec.Command(bin, args...)
	cmd.Env = append(os.Environ(), fmt.Sprintf("GOMAXPROCS=%d", c.ServerProcs))
	if c.GCTrace {
		// report.py derives the GC mark-phase time share from these lines.
		cmd.Env = append(cmd.Env, "GODEBUG=gctrace=1")
	}
	cmd.Stdout, cmd.Stderr = s.logf, s.logf
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	fmt.Fprintf(s.logf, "=== start %s %v at %s\n", bin, args, time.Now().Format(time.RFC3339))
	if err := cmd.Start(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cmd = cmd
	s.pid.Store(int64(cmd.Process.Pid))
	ex := make(chan struct{})
	s.exited = ex
	s.wanted.Store(false)
	s.mu.Unlock()
	go func() {
		err := cmd.Wait()
		close(ex)
		if !s.wanted.Load() {
			s.crashes.Add(1)
			s.g.events.add("server_crash", fmt.Sprintf("pid %d exited unexpectedly: %v", cmd.Process.Pid, err))
			// restart automatically unless the run is over
			if s.g.ctx.Err() == nil {
				time.Sleep(time.Second)
				_ = s.start()
			}
		}
	}()
	return nil
}

// stop sends sig and waits for exit (SIGKILL after 15s). Returns the time taken.
func (s *server) stop(sig syscall.Signal) time.Duration {
	s.mu.Lock()
	cmd, ex := s.cmd, s.exited
	s.mu.Unlock()
	s.wanted.Store(true)
	t0 := time.Now()
	_ = syscall.Kill(-cmd.Process.Pid, sig)
	select {
	case <-ex:
	case <-time.After(15 * time.Second):
		s.g.events.add("server_kill9", "did not exit after signal")
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-ex
	}
	return time.Since(t0)
}

type restartRec struct {
	K              int     `json:"k"`
	KillAtS        float64 `json:"kill_at_s"`
	ExitMs         int64   `json:"exit_ms"`
	ReadyAfterS    float64 `json:"ready_after_kill_s"`
	PreUp          int64   `json:"pre_up"`
	T90FromReadyS  float64 `json:"t90_from_ready_s"`
	T99FromReadyS  float64 `json:"t99_from_ready_s"`
	T99FromKillS   float64 `json:"t99_from_kill_s"`
	T100FromReadyS float64 `json:"t100_from_ready_s"`
	Reached99      bool    `json:"reached_99"`
	PeakUp         int64   `json:"peak_up"`
}

func (g *gen) restart(k int) {
	pre := g.up.Load()
	g.met.restartActive.Store(true)
	killAt := time.Now()
	g.events.add("restart_kill", fmt.Sprintf("k=%d pre_up=%d", k, pre))
	var exit time.Duration
	var tp *proc
	sig := syscall.SIGTERM
	if g.cfg.RestartMode == "kill" {
		sig = syscall.SIGKILL
	}
	if g.topo != nil && g.cfg.RestartTarget != "server" {
		tp = g.topo.target()
		g.events.add("restart_target", tp.name)
		exit = g.topo.stopProc(tp, sig)
	} else if g.topo != nil {
		tp = g.topo.bes[0]
		exit = g.topo.stopProc(tp, sig)
	} else {
		exit = g.srv.stop(sig)
	}
	time.Sleep(g.cfg.RestartGap)
	var err error
	if tp != nil {
		err = g.topo.startProc(tp)
	} else {
		err = g.srv.start()
	}
	if err != nil {
		g.events.add("restart_error", err.Error())
		return
	}
	g.waitHealth(60 * time.Second)
	ready := time.Now()
	rec := restartRec{K: k, KillAtS: killAt.Sub(g.start).Seconds(), ExitMs: exit.Milliseconds(), ReadyAfterS: ready.Sub(killAt).Seconds(), PreUp: pre}
	g.events.add("restart_ready", fmt.Sprintf("k=%d after=%.1fs", k, rec.ReadyAfterS))
	t90, t99, t100 := 0.0, 0.0, 0.0
	deadline := ready.Add(5 * time.Minute)
	for time.Now().Before(deadline) && g.ctx.Err() == nil {
		u := g.up.Load()
		if u > rec.PeakUp {
			rec.PeakUp = u
		}
		el := time.Since(ready).Seconds()
		if t90 == 0 && float64(u) >= 0.9*float64(pre) {
			t90 = el
		}
		if t99 == 0 && float64(u) >= 0.99*float64(pre) {
			t99 = max(el, 0.001)
			rec.Reached99 = true
		}
		if t100 == 0 && u >= pre-int64(g.cfg.Flappers) {
			t100 = el
		}
		if t99 != 0 && (t100 != 0 || el > t99+15) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	rec.T90FromReadyS, rec.T99FromReadyS, rec.T100FromReadyS = t90, t99, t100
	rec.T99FromKillS = t99 + rec.ReadyAfterS
	g.events.add("restart_done", fmt.Sprintf("k=%d t99=%.1fs t100=%.1fs reached99=%v", k, t99, t100, rec.Reached99))
	g.met.addRestart(rec)
	time.AfterFunc(20*time.Second, func() { g.met.restartActive.Store(false) })
}

// freezeWatch detects a stalled server: a canary Heartbeat in flight for longer
// than the threshold, or /control/health stalling, while the process is alive.
func (g *gen) freezeWatch() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	frozen := false
	var since time.Time
	dumped := 0
	healthFail := 0
	for {
		select {
		case <-g.ctx.Done():
			return
		case <-t.C:
		}
		if g.met.restartActive.Load() && g.srv != nil && !g.srvUp() {
			continue
		}
		stalled := false
		why := ""
		for _, cn := range g.canaries {
			if hs := cn.hbStart.Load(); hs != 0 && time.Since(time.Unix(0, hs)) > g.cfg.FreezeThresh {
				stalled, why = true, fmt.Sprintf("%s heartbeat in flight %.1fs", cn.id, time.Since(time.Unix(0, hs)).Seconds())
			}
		}
		if d, ok := g.health(3 * time.Second); !ok && d >= 3*time.Second {
			healthFail++
			if healthFail >= 3 {
				stalled, why = true, "control/health stalled"
			}
		} else {
			healthFail = 0
		}
		if stalled && !frozen {
			frozen, since, dumped = true, time.Now(), 0
			g.events.add("FREEZE", why)
			g.met.addFreeze(g.since(), why)
		}
		if stalled && frozen {
			el := time.Since(since)
			if dumped == 0 || (dumped == 1 && el > 30*time.Second) {
				dumped++
				g.dumpGoroutines(fmt.Sprintf("freeze-%d", g.met.freezeCount()))
			}
			if el > 90*time.Second && g.srv != nil {
				g.events.add("freeze_sigquit", "freeze persisted 90s; SIGQUIT for stack in server.log, then restart")
				g.met.markFreezeUnrecovered()
				g.srv.stop(syscall.SIGQUIT)
				time.Sleep(time.Second)
				_ = g.srv.start()
				g.waitHealth(60 * time.Second)
				frozen = false
			}
		}
		if !stalled && frozen {
			g.events.add("freeze_recovered", fmt.Sprintf("after %.1fs", time.Since(since).Seconds()))
			g.met.freezeRecovered(time.Since(since).Seconds())
			frozen = false
		}
	}
}

func (g *gen) srvUp() bool { _, ok := g.health(time.Second); return ok }

func (g *gen) dumpGoroutines(name string) {
	if g.cfg.Pprof == "" {
		return
	}
	cl := &http.Client{Timeout: 30 * time.Second}
	resp, err := cl.Get("http://" + g.cfg.Pprof + "/debug/pprof/goroutine?debug=2")
	if err != nil {
		g.events.add("dump_error", err.Error())
		return
	}
	defer resp.Body.Close()
	f, _ := os.Create(filepath.Join(g.cfg.Out, "dumps", name+".txt"))
	defer f.Close()
	n, _ := io.Copy(f, resp.Body)
	g.events.add("dump", fmt.Sprintf("%s %d bytes", name, n))
}
