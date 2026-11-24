// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

// Package compat holds the switches of the gocpp compatibility module that have
// no counterpart in ocpp-go. It is the only package of the module that is not in
// ocpp-go, so that every ocpp-go package keeps exactly its upstream API.
//
// The runtime is selected with the environment variable OCPPGO_COMPAT_RUNTIME:
//
//	unset    the gocpp runtime for servers built without a custom ServerDispatcher
//	gocpp    the gocpp runtime also for an injected *ocppj.DefaultServerDispatcher
//	legacy   ocpp-go's original code path everywhere (operational fallback)
package compat

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/shiv3/gocpp/core/observability"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// Runtime reports which runtime serves s: "gocpp" or "legacy".
func Runtime(s *ocppj.Server) string {
	if _, ok := compatrt.Get(s, compatrt.KindRuntime); ok {
		return "gocpp"
	}
	return "legacy"
}

// SetCallTimeout sets how long an outbound request waits for its answer on a
// server that runs on the gocpp runtime, as ocppj.DefaultServerDispatcher's
// SetTimeout does for the original one (without injecting a dispatcher). Call
// it before Start. It returns false when s runs on the legacy runtime.
func SetCallTimeout(s *ocppj.Server, d time.Duration) bool {
	v, ok := compatrt.Get(s, compatrt.KindRuntime)
	if !ok {
		return false
	}
	c, ok := v.(compatrt.Control)
	if !ok {
		return false
	}
	c.SetCallTimeout(d)
	return true
}

// SetObservability attaches metrics, tracing and a raw frame observer to the
// connections of s. Pass nil for what is not wanted. Call it before Start. It
// returns false when s runs on the legacy runtime (nothing is attached).
//
//   - m receives gocpp's metrics: connection count, calls started and
//     completed per action and direction with their duration and status, pending
//     calls, schema failures. If m also implements observability.SessionMetrics
//     (the Prometheus and OpenTelemetry implementations in
//     github.com/shiv3/gocpp/core/observability/metrics do), it also gets the
//     result of every upgrade attempt and the close code and cause of every
//     connection, which is what a canary comparison needs.
//   - tp is the OpenTelemetry tracer provider; handler invocations and outbound
//     calls are spans.
//   - fo sees every raw frame, inbound (including unparsable ones) and outbound.
//     It runs inline and must be fast.
func SetObservability(s *ocppj.Server, m observability.Metrics, tp trace.TracerProvider, fo observability.FrameObserver) bool {
	c, ok := control(s)
	if !ok {
		return false
	}
	c.SetObservability(m, tp, fo)
	return true
}

func control(s *ocppj.Server) (compatrt.Control, bool) {
	v, ok := compatrt.Get(s, compatrt.KindRuntime)
	if !ok {
		return nil, false
	}
	c, ok := v.(compatrt.Control)
	return c, ok
}

func native(s *ws.Server) (compatrt.Native, bool) {
	v, ok := compatrt.Get(s, compatrt.KindNative)
	if !ok {
		return nil, false
	}
	n, ok := v.(compatrt.Native)
	return n, ok
}

// Admission limits how connections are accepted by a ws.Server. A zero value
// leaves the limit off.
type Admission struct {
	// MaxConnections caps the number of connected charge points. An upgrade over
	// the cap is refused before the WebSocket handshake with HTTP 503 and a
	// Retry-After header, so the charge point sees an ordinary failed connect and
	// backs off. A charge point reconnecting under an id that is already connected
	// replaces its old connection and is never refused because of the cap.
	MaxConnections int
	// AcceptRate and AcceptBurst are a token bucket on new upgrades (per second
	// and burst size). An upgrade over the limit is answered with 503 and a
	// jittered Retry-After before authentication or any other work, so a
	// reconnect storm cannot turn into a CPU or database storm.
	AcceptRate  float64
	AcceptBurst int
	// SoftMaxConnections makes the handler of ReadyHandler report not-ready at or
	// above that many connections, while upgrades are still accepted up to
	// MaxConnections: a load balancer stops adding to a process that has its share.
	SoftMaxConnections int
}

// SetAdmission sets the admission limits of s. Call it before Start. It returns
// false when s is not the ws.Server of this module (it always is, in a build
// that uses it).
func SetAdmission(s *ws.Server, a Admission) bool {
	n, ok := native(s)
	if !ok {
		return false
	}
	n.SetAdmission(compatrt.Admission{MaxConnections: a.MaxConnections, AcceptRate: a.AcceptRate,
		AcceptBurst: a.AcceptBurst, SoftMax: a.SoftMaxConnections})
	return true
}

// DuplicatePolicy says what a ws.Server does with a second connection for the id
// of a connected charge point.
type DuplicatePolicy int

const (
	// DuplicateFirstWins is what ocpp-go v0.19.0 does and the default of this
	// module: the old connection stays and the new one is upgraded, then closed
	// with 1008 ("a connection with this ID already exists"). A charge point
	// reconnecting after a half-open drop is refused until the server times the
	// old socket out.
	DuplicateFirstWins DuplicatePolicy = iota
	// DuplicateLastWins is gocpp's own policy: the new connection takes over the
	// id and the old one is closed with 1008. A charge point that lost its
	// connection without the server noticing is never locked out.
	DuplicateLastWins
)

// SetDuplicatePolicy selects the duplicate id policy of s. It returns false when s
// is not the module's own server.
func SetDuplicatePolicy(s *ws.Server, p DuplicatePolicy) bool {
	n, ok := native(s)
	if !ok {
		return false
	}
	n.SetFirstWins(p == DuplicateFirstWins)
	return true
}

// IdleClose says how a ws.Server ends a connection that was idle for
// ServerTimeoutConfig.PingWait.
type IdleClose int

const (
	// IdleCloseDrop is what ocpp-go v0.19.0 does and the default: the TCP
	// connection is dropped without a close frame (the charge point sees close
	// code 1006).
	IdleCloseDrop IdleClose = iota
	// IdleCloseFrame sends a close frame with code 1000 first.
	IdleCloseFrame
)

// SetIdleClose selects how s closes an idle connection. Call it before Start. It
// returns false when s is not the module's own server.
func SetIdleClose(s *ws.Server, c IdleClose) bool {
	n, ok := native(s)
	if !ok {
		return false
	}
	n.SetIdleCloseFrame(c == IdleCloseFrame)
	return true
}

// SetMaxMessageSize limits the size of an inbound WebSocket message of s, in
// bytes; a larger message closes the connection with 1009. The default, 0, is
// ocpp-go v0.19.0's behaviour: no limit. It applies to connections accepted
// afterwards. It returns false when s is not the module's own server.
func SetMaxMessageSize(s *ws.Server, n int64) bool {
	nat, ok := native(s)
	if !ok {
		return false
	}
	nat.SetMaxMessageSize(n)
	return true
}

// SetSoftMaxConnections changes the soft cap while the server runs (0 removes it).
func SetSoftMaxConnections(s *ws.Server, n int) {
	if nat, ok := native(s); ok {
		nat.SetSoftMax(n)
	}
}

// AdmissionStats counts the upgrades refused before they were accepted.
type AdmissionStats struct {
	RejectedRateLimit, RejectedDraining, RejectedFull uint64
}

// Stats returns the refusal counters of s.
func Stats(s *ws.Server) AdmissionStats {
	nat, ok := native(s)
	if !ok {
		return AdmissionStats{}
	}
	st := nat.AdmissionStats()
	return AdmissionStats{RejectedRateLimit: st.RejectedRateLimit, RejectedDraining: st.RejectedDraining, RejectedFull: st.RejectedFull}
}

// Connections returns the number of connected charge points of s.
func Connections(s *ws.Server) int {
	if nat, ok := native(s); ok {
		return nat.Connections()
	}
	return 0
}

// ReadyHandler returns a readiness handler for s: 200 while the server wants new
// connections, 503 once Drain or Stop has begun and while the number of
// connections is at or above the soft cap. Mount it with s.AddHttpHandler.
// Established connections are never affected by readiness.
func ReadyHandler(s *ws.Server) http.Handler {
	if nat, ok := native(s); ok {
		return nat.Ready()
	}
	return http.NotFoundHandler()
}

// DrainOptions tunes Drain. Zero values select the defaults.
type DrainOptions struct {
	// Window is the time in which every connection is closed, and the hard
	// limit: connections still open when it ends are closed regardless (default 5
	// minutes).
	Window time.Duration
	// Rate is the number of connections closed per second. Zero means
	// connections / (0.9 x Window): 10,000 chargers and a 5 minute window leave at
	// about 37 per second.
	Rate float64
	// IdleGrace is how long a connection that is busy (a request in flight in
	// either direction) when its turn comes is left alone, with no new outbound
	// request started on it, before it is closed anyway (default 30 s). Idle
	// connections are always closed first.
	IdleGrace time.Duration
	// RetryAfter is the base of the jittered Retry-After header (seconds) of the
	// 503 that refuses new upgrades while draining (default 10 s).
	RetryAfter time.Duration
	// OnProgress is called about once a second and when the drain ends.
	OnProgress func(DrainProgress)
}

// DrainProgress is a snapshot of a running drain.
type DrainProgress struct {
	Total, Closed, Forced, Remaining int
	Elapsed                          time.Duration
}

// ErrDrainInProgress is returned by Drain when a drain is already running.
var ErrDrainInProgress = compatrt.ErrDrainInProgress

// Drain moves every charge point off this process without a reconnect storm,
// for a deploy or a planned shutdown. New upgrades are refused with 503 and a
// jittered Retry-After from the moment Drain is called (and ReadyHandler
// reports not-ready, so a load balancer stops sending). The connections are
// closed at a fixed rate, idle ones first, with WebSocket close code 1012
// (service restart); a connection with a request in flight is passed over until
// that finishes, or for IdleGrace. Drain returns when every connection is gone,
// or with ctx.Err() if ctx ends first. It does not stop the server: call Stop
// afterwards. Upgrades stay refused until Undrain.
//
// In a graceful shutdown call Drain first and ws.Server.Stop after it; Stop on
// its own closes every connection at once (close code 1000) and so invites the
// reconnect storm that Drain avoids.
func Drain(ctx context.Context, s *ws.Server, opts DrainOptions) error {
	nat, ok := native(s)
	if !ok {
		return nil
	}
	o := compatrt.DrainOptions{Window: opts.Window, Rate: opts.Rate, IdleGrace: opts.IdleGrace, RetryAfter: opts.RetryAfter}
	if opts.OnProgress != nil {
		o.OnProgress = func(p compatrt.DrainProgress) { opts.OnProgress(DrainProgress(p)) }
	}
	return nat.Drain(ctx, o)
}

// Draining reports whether s refuses upgrades because Drain was called.
func Draining(s *ws.Server) bool {
	nat, ok := native(s)
	return ok && nat.Draining()
}

// Undrain makes s accept upgrades again after Drain (for example when a
// rollout was cancelled).
func Undrain(s *ws.Server) {
	if nat, ok := native(s); ok {
		nat.Undrain()
	}
}
