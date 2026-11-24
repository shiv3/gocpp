// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

// Package compatrt connects the packages of the compatibility module without
// widening their exported API.
//
// ocpp-go's packages are layered ws -> ocppj -> ocpp1.6 / ocpp2.0.1 and the
// compatibility module must keep their exported API identical to the pinned
// upstream tag (checked by apidiff). Where one layer needs to talk to another
// beyond that API (the gocpp runtime in ocppj taking over the sockets of ws.Server,
// the 1.6 / 2.0.1 glue asking whether handlers already run on a gocpp handler
// goroutine), the facts are exchanged here, keyed by the object they concern.
//
// This package is internal to the module and carries no stability promise.
package compatrt

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"go.opentelemetry.io/otel/trace"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/transport"
)

// Channel is the method set of ws.Channel.
type Channel interface {
	ID() string
	RemoteAddr() net.Addr
	TLSConnectionState() *tls.ConnectionState
}

// ServerSettings is the part of ws.ServerTimeoutConfig the runtime needs.
type ServerSettings struct {
	WriteWait      time.Duration
	PingWait       time.Duration
	MaxMessageSize int64
	// IdleCloseFrame: a connection closed for being idle gets a close frame (1000)
	// instead of being dropped (ocpp-go's behaviour, the default).
	IdleCloseFrame bool
}

// ConnTimeouts overrides the server-wide timeouts of one connection (0 = the server's).
type ConnTimeouts struct {
	// CallTimeout is how long an outbound request waits for its answer.
	CallTimeout time.Duration
	// IdleTimeout closes the connection when no frame (data, ping or pong) arrives
	// within it; the ws.ServerTimeoutConfig.PingWait of this connection.
	IdleTimeout time.Duration
}

// Compression selects permessage-deflate for accepted connections.
type Compression int

const (
	CompressionDisabled Compression = iota // default, as ocpp-go
	CompressionContextTakeover
	CompressionNoContextTakeover
)

// Conn is what a runtime gets for an accepted WebSocket connection.
type Conn struct {
	Timeouts    ConnTimeouts
	Channel     Channel
	WS          transport.WS
	Subprotocol string
	RemoteAddr  string
	TLS         *tls.ConnectionState
}

// Runtime serves the connections a native ws.Server hands over instead of
// running its own read loop: the ocppj server on the gocpp dispatcher.
type Runtime interface {
	// Open creates and starts the connection's dispatcher with inbound
	// processing on hold; the caller releases it after the application has heard
	// about the new connection.
	Open(c Conn) *dispatcher.Conn
}

// Native is implemented (by an unexported type) by the ws.Server of this module.
type Native interface {
	// SetRuntime attaches rt (nil detaches it). Connections accepted from then on
	// are handed to it.
	SetRuntime(rt Runtime)
	// Settings returns the server's current timeouts and limits.
	Settings() ServerSettings

	// The rest backs the compat package (admission, drain, readiness).

	// SetAdmission replaces the admission limits; call before Start. The soft cap
	// can also be changed later with SetSoftMax.
	SetAdmission(a Admission)
	SetSoftMax(n int)
	AdmissionStats() AdmissionStats
	// Ready is the readiness handler: 503 while draining, stopping or at the soft cap.
	Ready() http.Handler
	// Connections is the number of connected charge points.
	Connections() int
	// Drain closes every connection at a paced rate (see DrainOptions) and keeps
	// refusing upgrades until Undrain.
	Drain(ctx context.Context, opts DrainOptions) error
	Undrain()
	Draining() bool
	// SetUpgradeObserver receives the result of every upgrade attempt: "ok",
	// "rate_limited", "draining", "shutting_down", "full", "auth_rejected",
	// "bad_request" or "subprotocol" or "duplicate".
	SetUpgradeObserver(fn func(result string))
	// SetFirstWins selects the duplicate id policy: false lets a new
	// connection take over the id and closes the old one; true (default) keeps the old
	// connection and closes the new one with 1008, as ocpp-go v0.19.0 does (the default).
	SetFirstWins(on bool)
	// SetIdleCloseFrame selects the close of an idle connection: false (default)
	// drops the TCP connection as ocpp-go does, true sends a close frame (1000).
	// Call before Start.
	SetIdleCloseFrame(on bool)
	// ConnectionIDs lists the ids of the connected charge points.
	ConnectionIDs() []string
	// Handler is the websocket upgrade handler of the server (id = last path
	// segment), for mounting on an external mux.
	Handler() http.Handler
	// PrepareExternal readies the server for Handler without listening, as
	// Start does before it listens; Stop shuts it down.
	PrepareExternal()
	// SetBasicAuthIDHandler sets a basic auth check that also gets the charge point id.
	SetBasicAuthIDHandler(h func(id, username, password string) bool)
	// SetIDResolver derives the charge point id from the request (pathID is the
	// last path segment); ok=false refuses the upgrade with 404.
	SetIDResolver(f func(r *http.Request, pathID string) (id string, ok bool))
	// SetCompression selects permessage-deflate for connections accepted afterwards.
	SetCompression(c Compression)
	// SetConnTimeouts sets per-connection timeouts, asked once per upgrade.
	SetConnTimeouts(f func(r *http.Request, id string) ConnTimeouts)
	// SetMaxMessageSize limits inbound WebSocket messages (0 = unlimited, the
	// default and ocpp-go's behaviour). Affects connections accepted afterwards.
	SetMaxMessageSize(n int64)
}

// Admission limits upgrades (zero values: no limit).
type Admission struct {
	MaxConnections int
	AcceptRate     float64
	AcceptBurst    int
	SoftMax        int
}

// AdmissionStats counts upgrades refused before they were accepted.
type AdmissionStats struct {
	RejectedRateLimit, RejectedDraining, RejectedFull uint64
}

type slot struct {
	key  uintptr
	kind string
}

var (
	mu   sync.Mutex
	vals = map[slot]any{}
)

// Set associates v with the object p points to under kind, for the object's lifetime.
func Set[T any](p *T, kind string, v any) {
	if p == nil {
		return
	}
	k := slot{uintptr(unsafe.Pointer(p)), kind}
	mu.Lock()
	_, existed := vals[k]
	vals[k] = v
	mu.Unlock()
	if !existed {
		runtime.AddCleanup(p, func(k slot) {
			mu.Lock()
			delete(vals, k)
			mu.Unlock()
		}, k)
	}
}

// Get returns the value associated with p under kind by Set.
func Get[T any](p *T, kind string) (any, bool) {
	if p == nil {
		return nil, false
	}
	mu.Lock()
	v, ok := vals[slot{uintptr(unsafe.Pointer(p)), kind}]
	mu.Unlock()
	return v, ok
}

// Kinds of association.
const (
	KindNative  = "native"  // *ws.Server -> Native
	KindRuntime = "runtime" // *ocppj.Server -> its runtime
	kindInline  = "inline"
)

// MarkInline records that the server p serves inbound requests on gocpp handler
// goroutines (which recover panics), so that the layer above must not start its
// own goroutine for them.
func MarkInline[T any](p *T) { Set(p, kindInline, true) }

// Inline reports whether MarkInline was called for p.
func Inline[T any](p *T) bool {
	_, ok := Get(p, kindInline)
	return ok
}

// Safely runs f and recovers from a panic, reporting it through onPanic.
func Safely(f func(), onPanic func(r any)) {
	defer func() {
		if r := recover(); r != nil && onPanic != nil {
			onPanic(r)
		}
	}()
	f()
}

// Control is implemented by the gocpp runtime of an ocppj.Server (stored under KindRuntime).
type Control interface {
	SetCallTimeout(d time.Duration)
	// SetValidationErrorToCallback makes an answer that fails parsing or
	// validation complete the request's callback at once instead of at the call
	// timeout. Call before Start.
	SetValidationErrorToCallback(on bool)
	// SetObservability sets what the connections of the runtime report to; any
	// nil argument leaves that facility off. Call before Start.
	SetObservability(m observability.Metrics, tp trace.TracerProvider, fo observability.FrameObserver)
	// SetValidation installs the validation error filter and the 1.6 CALLERROR
	// spelling switch (nil / false = upstream behaviour). Safe at any time.
	SetValidation(filter ValidationFilter, specSpellingV16 bool)
	// SetDecoding installs the opt-in payload decoding options (zero value =
	// upstream behaviour). Safe at any time.
	SetDecoding(d Decoding)
	// StartExternal starts the runtime like Start, but without listening: the
	// application serves the ws.Server's Handler itself.
	StartExternal(native Native)
}

// Decoding are the opt-in payload decoding quirks of the gocpp runtime.
type Decoding struct {
	// UseNumber decodes payload numbers without going through float64, so
	// integers beyond 2^53 reach the struct unrounded.
	UseNumber bool
	// NumericSampledValue accepts a JSON number as OCPP 1.6 SampledValue.Value.
	NumericSampledValue bool
	// LenientDateTime repairs near-RFC3339 timestamps (space separator, missing
	// zone, "+0100", comma fraction, date only) before they are parsed.
	LenientDateTime bool
}

// Any reports whether any option is set.
func (d Decoding) Any() bool { return d != Decoding{} }

// ValidationFilter decides whether one field validation failure of a message is
// tolerated: action is the OCPP action, field the path of the field below the
// payload (for example "ConfigurationKey[0].Value", Go field names) and tag the
// failed validator tag ("required", "min", "max", ...).
type ValidationFilter func(action, field, tag string) bool
