// Package admin is the operator interface of one gateway process for staged
// releases of the connection tier (docs/design/gateway.md 5.2): a small HTTP
// handler (status, shed, soft cap) that cmd/gocpp-gateway mounts on its internal
// admin listener, the matching client, and the planner gocppctl uses to compute
// a rebalance. It moves sessions only through csms.Server.Drain (idle first,
// paced, close code 1012), never by touching a socket itself.
//
// Experimental: this API may change before v1.0. Every exported identifier of
// this package is part of the split-gateway API; see "Stability" in
// docs/gateway.md.
package admin

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/shiv3/gocpp/csms"
)

// DefaultMinSessionAge is how old a session must be to be shed: one charger is
// not moved repeatedly (docs/design/gateway.md 5.2.2).
const DefaultMinSessionAge = 10 * time.Minute

// Status is a gateway's live state.
type Status struct {
	ID           string           `json:"id"`
	Version      string           `json:"version"`
	Sessions     int              `json:"sessions"`
	SoftMax      int              `json:"soft_max"`
	Draining     bool             `json:"draining"`
	PartialDrain bool             `json:"partial_drain"`
	Drain        csms.DrainStatus `json:"drain"`
}

// ShedRequest asks a gateway to close sessions at a paced rate so that the
// chargers reconnect elsewhere.
type ShedRequest struct {
	// Count is how many sessions to close (> 0).
	Count int `json:"count"`
	// MinAge skips sessions younger than this (default DefaultMinSessionAge;
	// a negative value disables the check).
	MinAge Duration `json:"min_age,omitempty"`
	// Attrs restricts the shed to sessions whose attributes equal all pairs.
	Attrs map[string]string `json:"attrs,omitempty"`
	// Rate is sessions closed per second; 0 spreads Count over Window.
	Rate float64 `json:"rate,omitempty"`
	// Window is the time the shed is spread over (default 5 minutes).
	Window Duration `json:"window,omitempty"`
	// RefuseDuring also answers new upgrades with 503 while shedding.
	RefuseDuring bool `json:"refuse_during,omitempty"`
}

// Duration reads and writes "10m" style strings in JSON.
type Duration time.Duration

// MarshalJSON implements json.Marshaler.
func (d Duration) MarshalJSON() ([]byte, error) { return json.Marshal(time.Duration(d).String()) }

// UnmarshalJSON implements json.Unmarshaler.
func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("duration must be a string like \"10m\": %w", err)
	}
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(v)
	return nil
}

// Options configures Handler.
type Options struct {
	ID      string
	Version string
	// Token, when set, is required as "Authorization: Bearer <token>".
	Token string
	// Context bounds shed drains (default: context.Background).
	Context context.Context
}

// Handler serves the gateway's admin API on srv:
//
//	GET  /admin/status    Status
//	POST /admin/shed      ShedRequest -> 202; 409 while a drain runs
//	POST /admin/soft-max  {"n": N} sets the soft session cap (0 removes it)
func Handler(srv *csms.Server, o Options) http.Handler {
	if o.Context == nil {
		o.Context = context.Background()
	}
	h := &handler{srv: srv, o: o}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /admin/status", h.status)
	mux.HandleFunc("POST /admin/shed", h.shed)
	mux.HandleFunc("POST /admin/soft-max", h.softMax)
	return h.auth(mux)
}

type handler struct {
	srv *csms.Server
	o   Options
}

func (h *handler) auth(next http.Handler) http.Handler { return RequireToken(h.o.Token, next) }

// RequireToken wraps next so that, when token is not empty, a request must carry
// "Authorization: Bearer <token>". An empty token disables the check; callers
// that listen beyond loopback must not pass one (cmd/gocpp-gateway refuses to).
// The comparison is constant time.
func RequireToken(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		const prefix = "Bearer "
		h := r.Header.Get("Authorization")
		got, ok := "", len(h) > len(prefix) && strings.EqualFold(h[:len(prefix)], prefix)
		if ok {
			got = h[len(prefix):]
		}
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			w.Header().Set("WWW-Authenticate", "Bearer")
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (h *handler) snapshot() Status {
	return Status{
		ID: h.o.ID, Version: h.o.Version, Sessions: len(h.srv.Connections()), SoftMax: h.srv.SoftMaxConnections(),
		Draining: h.srv.Draining(), PartialDrain: h.srv.DrainStatus().Active && h.srv.DrainStatus().Partial,
		Drain: h.srv.DrainStatus(),
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (h *handler) status(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.snapshot())
}

func (h *handler) softMax(w http.ResponseWriter, r *http.Request) {
	var body struct {
		N int `json:"n"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body); err != nil || body.N < 0 {
		http.Error(w, "body must be {\"n\": N} with N >= 0", http.StatusBadRequest)
		return
	}
	h.srv.SetSoftMaxConnections(body.N)
	writeJSON(w, http.StatusOK, h.snapshot())
}

func (h *handler) shed(w http.ResponseWriter, r *http.Request) {
	var req ShedRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || req.Count <= 0 {
		http.Error(w, "body must be a ShedRequest with count > 0", http.StatusBadRequest)
		return
	}
	minAge := time.Duration(req.MinAge)
	if minAge == 0 {
		minAge = DefaultMinSessionAge
	}
	window := time.Duration(req.Window)
	if window <= 0 {
		window = 5 * time.Minute
	}
	opts := csms.DrainOptions{Max: req.Count, Attrs: req.Attrs, Rate: req.Rate, Window: window, RefuseDuring: req.RefuseDuring}
	if minAge > 0 {
		opts.Filter = func(c *csms.Conn) bool { return time.Since(c.Stats().ConnectedAt) >= minAge }
	}
	started := make(chan error, 1)
	go func() {
		// Drain returns ErrDrainInProgress at once when another one runs; report
		// that to the caller instead of 202.
		err := h.srv.Drain(h.o.Context, opts)
		select {
		case started <- err:
		default:
		}
	}()
	select {
	case err := <-started:
		if errors.Is(err, csms.ErrDrainInProgress) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		// Finished already (nothing to shed).
		writeJSON(w, http.StatusAccepted, h.snapshot())
	case <-time.After(100 * time.Millisecond):
		writeJSON(w, http.StatusAccepted, h.snapshot())
	}
}
