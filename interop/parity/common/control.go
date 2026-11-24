package common

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"time"
)

// ErrUnsupported is returned by a Backend for operations its library does not
// expose publicly.
var ErrUnsupported = errors.New("unsupported")

// CallError is a CALLERROR answer from the charge point.
type CallError struct{ Code, Description string }

func (e *CallError) Error() string { return "call error " + e.Code + ": " + e.Description }

// Error kinds for KindError.
const (
	KindTimeout      = "timeout"
	KindNotConnected = "not_connected"
	KindInvalid      = "invalid_request"
	KindOther        = "other"
)

// KindedError carries a control-API error kind.
type KindedError struct {
	Kind string
	Err  error
}

func (e *KindedError) Error() string { return e.Err.Error() }
func (e *KindedError) Unwrap() error { return e.Err }

// Backend is the library-specific half of the control API.
type Backend interface {
	Lib() string
	Version() string
	// Call sends a CSMS-initiated request and returns the raw result payload.
	// ctx carries the call timeout. Errors: *CallError, *KindedError, others.
	Call(ctx context.Context, cpID, action string, payload json.RawMessage) (json.RawMessage, error)
	// Raw writes an arbitrary text frame; return ErrUnsupported if impossible.
	Raw(cpID, frame string) error
	// Disconnect closes one connection from the server side.
	Disconnect(cpID string) error
	// Shutdown gracefully closes every charge point connection.
	Shutdown() error
}

// Control returns the handler for everything under /control/.
func Control(rec *Recorder, b Backend, callTimeout time.Duration) http.Handler {
	h := &control{rec: rec, b: b, timeout: callTimeout}
	return h
}

type control struct {
	rec     *Recorder
	b       Backend
	timeout time.Duration
}

type obj = map[string]any

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (c *control) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimSuffix(r.URL.Path, "/")
	need := func(method string) bool {
		if r.Method != method {
			writeJSON(w, http.StatusMethodNotAllowed, obj{"ok": false, "error": "method not allowed", "kind": KindInvalid})
			return false
		}
		return true
	}
	switch path {
	case "/control/health":
		writeJSON(w, 200, obj{"ok": true, "lib": c.b.Lib(), "version": c.b.Version()})
	case "/control/count":
		cnt, n := c.rec.Counts()
		writeJSON(w, 200, obj{"ok": true, "connected": n, "counts": cnt})
	case "/control/runtime":
		writeJSON(w, 200, runtimeSnapshot())
	case "/control/connections":
		writeJSON(w, 200, c.rec.Connections())
	case "/control/events":
		writeJSON(w, 200, c.rec.Events())
	case "/control/events/reset":
		if need(http.MethodPost) {
			c.rec.Reset()
			writeJSON(w, 200, obj{"ok": true})
		}
	case "/control/call":
		if need(http.MethodPost) {
			c.call(w, r)
		}
	case "/control/raw":
		if need(http.MethodPost) {
			c.raw(w, r)
		}
	case "/control/disconnect":
		if need(http.MethodPost) {
			c.disconnect(w, r)
		}
	case "/control/shutdown":
		if need(http.MethodPost) {
			if err := c.b.Shutdown(); err != nil {
				writeJSON(w, 200, errBody(err))
				return
			}
			writeJSON(w, 200, obj{"ok": true})
		}
	default:
		writeJSON(w, 404, obj{"ok": false, "error": "not found", "kind": KindInvalid})
	}
}

func decode(r *http.Request, v any) error {
	b, err := io.ReadAll(io.LimitReader(r.Body, 8<<20))
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

func errBody(err error) obj {
	var ce *CallError
	var ke *KindedError
	switch {
	case errors.Is(err, ErrUnsupported):
		return obj{"ok": false, "error": "unsupported", "kind": KindOther}
	case errors.As(err, &ce):
		return obj{"ok": false, "callError": obj{"code": ce.Code, "description": ce.Description}}
	case errors.Is(err, context.DeadlineExceeded):
		return obj{"ok": false, "error": err.Error(), "kind": KindTimeout}
	case errors.As(err, &ke):
		return obj{"ok": false, "error": ke.Error(), "kind": ke.Kind}
	}
	return obj{"ok": false, "error": err.Error(), "kind": KindOther}
}

func (c *control) call(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CpID    string          `json:"cpId"`
		Action  string          `json:"action"`
		Payload json.RawMessage `json:"payload"`
	}
	if err := decode(r, &req); err != nil || req.CpID == "" || req.Action == "" {
		writeJSON(w, 200, obj{"ok": false, "error": "bad request body", "kind": KindInvalid})
		return
	}
	if len(req.Payload) == 0 || string(req.Payload) == "null" {
		req.Payload = json.RawMessage(`{}`)
	}
	if !c.rec.IsConnected(req.CpID) {
		writeJSON(w, 200, obj{"ok": false, "error": "charge point not connected", "kind": KindNotConnected})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), c.timeout)
	defer cancel()
	res, err := c.b.Call(ctx, req.CpID, req.Action, req.Payload)
	if err != nil {
		writeJSON(w, 200, errBody(err))
		return
	}
	if len(res) == 0 {
		res = json.RawMessage(`{}`)
	}
	writeJSON(w, 200, obj{"ok": true, "result": res})
}

func (c *control) raw(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CpID  string `json:"cpId"`
		Frame string `json:"frame"`
	}
	if err := decode(r, &req); err != nil || req.CpID == "" {
		writeJSON(w, 200, obj{"ok": false, "error": "bad request body", "kind": KindInvalid})
		return
	}
	if err := c.b.Raw(req.CpID, req.Frame); err != nil {
		writeJSON(w, 200, errBody(err))
		return
	}
	writeJSON(w, 200, obj{"ok": true})
}

func (c *control) disconnect(w http.ResponseWriter, r *http.Request) {
	var req struct {
		CpID string `json:"cpId"`
	}
	if err := decode(r, &req); err != nil || req.CpID == "" {
		writeJSON(w, 200, obj{"ok": false, "error": "bad request body", "kind": KindInvalid})
		return
	}
	if !c.rec.IsConnected(req.CpID) {
		writeJSON(w, 200, obj{"ok": false, "error": "charge point not connected", "kind": KindNotConnected})
		return
	}
	if err := c.b.Disconnect(req.CpID); err != nil {
		writeJSON(w, 200, errBody(err))
		return
	}
	writeJSON(w, 200, obj{"ok": true})
}

// runtimeSample names the runtime/metrics the load-test harness samples. The
// JSON key is what the harness and report.py read.
var runtimeSample = []struct{ key, name string }{
	{"alloc_bytes", "/gc/heap/allocs:bytes"},
	{"gc_cycles", "/gc/cycles/total:gc-cycles"},
	{"gc_assist_cpu_s", "/cpu/classes/gc/mark/assist:cpu-seconds"},
	{"gc_total_cpu_s", "/cpu/classes/gc/total:cpu-seconds"},
	{"heap_objects_bytes", "/memory/classes/heap/objects:bytes"},
	{"heap_stacks_bytes", "/memory/classes/heap/stacks:bytes"},
}

// runtimeSnapshot reads runtime/metrics: cumulative counters (the harness takes
// deltas between samples), gauges, and the cumulative scheduler-latency
// histogram (bucket boundaries in seconds plus counts) from which the harness
// derives a per-interval p99.
func runtimeSnapshot() obj {
	samples := make([]metrics.Sample, 0, len(runtimeSample)+1)
	for _, m := range runtimeSample {
		samples = append(samples, metrics.Sample{Name: m.name})
	}
	samples = append(samples, metrics.Sample{Name: "/sched/latencies:seconds"})
	metrics.Read(samples)
	out := obj{"ok": true, "gomaxprocs": runtime.GOMAXPROCS(0), "numcpu": runtime.NumCPU(), "gomemlimit": debug.SetMemoryLimit(-1)}
	for i, m := range runtimeSample {
		switch v := samples[i].Value; v.Kind() {
		case metrics.KindUint64:
			out[m.key] = v.Uint64()
		case metrics.KindFloat64:
			out[m.key] = v.Float64()
		}
	}
	if v := samples[len(samples)-1].Value; v.Kind() == metrics.KindFloat64Histogram {
		h := v.Float64Histogram()
		// The first and last boundaries may be +-Inf, which JSON cannot carry.
		b := make([]float64, len(h.Buckets))
		for i, x := range h.Buckets {
			switch {
			case x > 1e9:
				x = 1e9
			case x < -1e9:
				x = -1e9
			}
			b[i] = x
		}
		out["sched_buckets"] = b
		out["sched_counts"] = h.Counts
	}
	return out
}
