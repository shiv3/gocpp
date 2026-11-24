package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/core/ocppj"
)

// Fault kinds.
const (
	// FaultLatency delays the answer by Delay.
	FaultLatency = "latency"
	// FaultError answers with a CALLERROR (ErrorCode, Description).
	FaultError = "error"
	// FaultIgnore never answers: the CALL is held until the connection ends.
	FaultIgnore = "ignore"
)

// ignoreMax bounds how long an ignored CALL is held.
const ignoreMax = 10 * time.Minute

// Rule is one fault. Action "" or "*" matches every action, CPID "" every
// charge point. Count > 0 limits the rule to that many matching CALLs.
type Rule struct {
	Action      string `json:"action,omitempty"`
	CPID        string `json:"cpId,omitempty"`
	Kind        string `json:"kind"`
	Delay       string `json:"delay,omitempty"`
	ErrorCode   string `json:"errorCode,omitempty"`
	Description string `json:"description,omitempty"`
	Count       int    `json:"count,omitempty"`

	delay time.Duration
}

// Faults is the runtime-configurable fault list.
type Faults struct {
	mu    sync.Mutex
	rules []Rule
}

// Set replaces the rules after validating them.
func (f *Faults) Set(rules []Rule) error {
	for i := range rules {
		r := &rules[i]
		switch r.Kind {
		case FaultLatency:
			d, err := time.ParseDuration(r.Delay)
			if err != nil || d <= 0 {
				return fmt.Errorf("rule %d: latency needs a positive delay such as \"500ms\"", i)
			}
			r.delay = d
		case FaultError:
			if r.ErrorCode == "" {
				r.ErrorCode = string(ocppj.ErrorCodeInternalError)
			}
		case FaultIgnore:
		case "duplicate":
			return fmt.Errorf("rule %d: duplicate responses cannot be produced: the library offers no way to write a frame twice", i)
		default:
			return fmt.Errorf("rule %d: kind must be latency, error or ignore", i)
		}
		if r.Count < 0 {
			return fmt.Errorf("rule %d: count must not be negative", i)
		}
	}
	f.mu.Lock()
	f.rules = append([]Rule(nil), rules...)
	f.mu.Unlock()
	return nil
}

// Rules returns a copy of the rules.
func (f *Faults) Rules() []Rule {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Rule{}, f.rules...)
}

// match returns the first rule for the call, consuming one use of a counted
// rule.
func (f *Faults) match(call inbound.Call) (Rule, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := range f.rules {
		r := &f.rules[i]
		if r.Action != "" && r.Action != "*" && r.Action != call.Action {
			continue
		}
		if r.CPID != "" && r.CPID != call.CPID {
			continue
		}
		out := *r
		if r.Count > 0 {
			r.Count--
			if r.Count == 0 {
				f.rules = append(f.rules[:i], f.rules[i+1:]...)
			}
		}
		return out, true
	}
	return Rule{}, false
}

// Middleware is the inbound.Middleware applying the rules.
func (f *Faults) Middleware(next inbound.Responder) inbound.Responder {
	return func(ctx context.Context, call inbound.Call) (json.RawMessage, error) {
		r, ok := f.match(call)
		if !ok {
			return next(ctx, call)
		}
		switch r.Kind {
		case FaultLatency:
			select {
			case <-time.After(r.delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			return next(ctx, call)
		case FaultError:
			return nil, ocppj.NewCallError(ocppj.ErrorCode(r.ErrorCode), r.Description, nil)
		default: // ignore
			select {
			case <-time.After(ignoreMax):
			case <-ctx.Done():
			}
			return nil, ctx.Err()
		}
	}
}

func (f *Faults) get(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteResult(w, f.Rules())
}

// put serves PUT /faults with {"rules":[...]} (replacing every rule).
func (f *Faults) put(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rules []Rule `json:"rules"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, fmt.Sprintf("decode faults: %v", err))
		return
	}
	if err := f.Set(body.Rules); err != nil {
		httpapi.WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	httpapi.WriteResult(w, f.Rules())
}

func (f *Faults) del(w http.ResponseWriter, _ *http.Request) {
	_ = f.Set(nil)
	httpapi.WriteResult(w, []Rule{})
}
