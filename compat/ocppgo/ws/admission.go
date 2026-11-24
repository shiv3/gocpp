// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. It holds the connection admission
// and drain support of ws.Server, which ocpp-go's API cannot express; the
// application reaches it through the compat package.

package ws

import (
	"context"
	"math"
	"math/rand/v2"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
)

// admissionState is the part of Server that limits and drains connections.
type admissionState struct {
	cfg       atomic.Pointer[compatrt.Admission]
	bucket    atomic.Pointer[tokenBucket]
	softMax   atomic.Int64
	firstWins atomic.Bool // duplicate id: keep the old connection, close the new one (1008)
	reserved  int         // upgrades admitted under MaxConnections, not yet registered; guarded by Server.connMutex

	draining   atomic.Bool
	retryAfter atomic.Int64 // ns
	drainMu    sync.Mutex   // one drain at a time
	rejRate    atomic.Uint64
	rejDrain   atomic.Uint64
	rejFull    atomic.Uint64
	observer   atomic.Pointer[func(string)]
}

// slotReservation is a connection slot held between admission and registration.
// held is guarded by Server.connMutex.
type slotReservation struct{ held bool }

func (n native) SetAdmission(a compatrt.Admission) {
	s := n.s
	a.MaxConnections = max(a.MaxConnections, 0)
	s.adm.cfg.Store(&a)
	s.adm.softMax.Store(int64(a.SoftMax))
	if a.AcceptRate > 0 {
		s.adm.bucket.Store(newTokenBucket(a.AcceptRate, a.AcceptBurst))
	} else {
		s.adm.bucket.Store(nil)
	}
}

func (n native) SetFirstWins(on bool) { n.s.adm.firstWins.Store(on) }

func (n native) SetSoftMax(v int) { n.s.adm.softMax.Store(int64(v)) }

func (n native) AdmissionStats() compatrt.AdmissionStats {
	a := &n.s.adm
	return compatrt.AdmissionStats{RejectedRateLimit: a.rejRate.Load(), RejectedDraining: a.rejDrain.Load(), RejectedFull: a.rejFull.Load()}
}

func (n native) Connections() int {
	n.s.connMutex.RLock()
	defer n.s.connMutex.RUnlock()
	return len(n.s.connections)
}

func (n native) SetUpgradeObserver(fn func(string)) {
	if fn == nil {
		n.s.adm.observer.Store(nil)
		return
	}
	n.s.adm.observer.Store(&fn)
}

func (n native) Draining() bool { return n.s.adm.draining.Load() }

func (n native) Undrain() { n.s.adm.draining.Store(false) }

func (n native) Ready() http.Handler {
	s := n.s
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		switch {
		case s.adm.draining.Load() || s.stopping.Load():
			http.Error(w, "draining", http.StatusServiceUnavailable)
		case s.overSoftMax():
			http.Error(w, "at soft connection limit", http.StatusServiceUnavailable)
		default:
			_, _ = w.Write([]byte("ok"))
		}
	})
}

func (s *Server) overSoftMax() bool {
	m := s.adm.softMax.Load()
	if m <= 0 {
		return false
	}
	s.connMutex.RLock()
	defer s.connMutex.RUnlock()
	return int64(len(s.connections)+s.adm.reserved) >= m
}

func (s *Server) upgradeResult(result string) {
	if f := s.adm.observer.Load(); f != nil {
		(*f)(result)
	}
}

func (s *Server) refuse(w http.ResponseWriter, msg, result string, retry time.Duration) {
	w.Header().Set("Retry-After", jitterRetryAfter(retry))
	http.Error(w, msg, http.StatusServiceUnavailable)
	s.upgradeResult(result)
}

// admit decides whether an upgrade for id may proceed: not while stopping or
// draining, not over the accept rate, not over MaxConnections (an id that is
// already connected replaces its old connection and needs no slot). On success
// the returned reservation must be passed to releaseSlot.
func (s *Server) admit(w http.ResponseWriter, id string) (*slotReservation, bool) {
	if s.stopping.Load() {
		s.refuse(w, "server shutting down", "shutting_down", 5*time.Second)
		return nil, false
	}
	if s.adm.draining.Load() {
		s.adm.rejDrain.Add(1)
		s.refuse(w, "draining", "draining", time.Duration(s.adm.retryAfter.Load()))
		return nil, false
	}
	cfg := s.adm.cfg.Load()
	if cfg == nil {
		return &slotReservation{}, true
	}
	if b := s.adm.bucket.Load(); b != nil && !b.allow(time.Now()) {
		s.adm.rejRate.Add(1)
		s.refuse(w, "too many connection attempts", "rate_limited", b.refillTime())
		return nil, false
	}
	res := &slotReservation{}
	if cfg.MaxConnections > 0 {
		s.connMutex.Lock()
		_, replacing := s.connections[id]
		if !replacing {
			if len(s.connections)+s.adm.reserved >= cfg.MaxConnections {
				s.connMutex.Unlock()
				s.adm.rejFull.Add(1)
				s.refuse(w, "server at capacity", "full", 10*time.Second)
				return nil, false
			}
			s.adm.reserved++
			res.held = true
		}
		s.connMutex.Unlock()
	}
	return res, true
}

// consumeSlotLocked turns the reservation into a registered connection. The caller holds connMutex.
func (s *Server) consumeSlotLocked(res *slotReservation) {
	if res != nil && res.held {
		res.held = false
		s.adm.reserved--
	}
}

func (s *Server) releaseSlot(res *slotReservation) {
	if res == nil {
		return
	}
	s.connMutex.Lock()
	s.consumeSlotLocked(res)
	s.connMutex.Unlock()
}

// Drain refuses new upgrades and closes every connection at the paced rate of
// opts, idle ones first, with close code 1012 (service restart).
func (n native) Drain(ctx context.Context, opts compatrt.DrainOptions) error {
	s := n.s
	if !s.adm.drainMu.TryLock() {
		return compatrt.ErrDrainInProgress
	}
	defer s.adm.drainMu.Unlock()
	s.connMutex.RLock()
	targets := make([]compatrt.DrainTarget, 0, len(s.connections))
	for _, w := range s.connections {
		targets = append(targets, drainTarget{w.serverConn})
	}
	s.connMutex.RUnlock()
	opts = opts.WithDefaults(len(targets))
	s.adm.retryAfter.Store(int64(opts.RetryAfter))
	s.adm.draining.Store(true)
	return compatrt.RunDrain(ctx, targets, opts)
}

// drainTarget adapts a connection to compatrt.DrainTarget.
type drainTarget struct{ sc *serverConn }

func closeServiceRestart(reason string) websocket.CloseError {
	return websocket.CloseError{Code: websocket.StatusServiceRestart, Reason: reason}
}

const drainReason = "server draining, please reconnect"

func (t drainTarget) CloseIfIdle() bool {
	if t.sc.dc != nil {
		return t.sc.dc.TryDrain()
	}
	return t.sc.requestClose(closeServiceRestart(drainReason)) == nil
}

func (t drainTarget) Quiesce() {
	if t.sc.dc != nil {
		t.sc.dc.Quiesce()
	}
}

func (t drainTarget) Unquiesce() {
	if t.sc.dc != nil {
		t.sc.dc.Unquiesce()
	}
}

func (t drainTarget) ForceClose() {
	if t.sc.dc != nil {
		t.sc.dc.ForceDrain()
		return
	}
	_ = t.sc.requestClose(closeServiceRestart(drainReason))
}

func (t drainTarget) Done() <-chan struct{} {
	if t.sc.dc != nil {
		return t.sc.dc.Context().Done()
	}
	return t.sc.done
}

// jitterRetryAfter renders a Retry-After value spread over 0.5x to 1.5x of
// base, at least one second.
func jitterRetryAfter(base time.Duration) string {
	if base <= 0 {
		base = 10 * time.Second
	}
	return strconv.Itoa(max(1, int(math.Round(base.Seconds()*(0.5+rand.Float64())))))
}

type tokenBucket struct {
	mu     sync.Mutex
	rate   float64
	burst  float64
	tokens float64
	last   time.Time
}

func newTokenBucket(rate float64, burst int) *tokenBucket {
	b := math.Max(1, float64(burst))
	return &tokenBucket{rate: rate, burst: b, tokens: b}
}

func (b *tokenBucket) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.last.IsZero() {
		b.tokens = math.Min(b.burst, b.tokens+b.rate*now.Sub(b.last).Seconds())
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (b *tokenBucket) refillTime() time.Duration {
	return time.Duration(b.burst / b.rate * float64(time.Second))
}
