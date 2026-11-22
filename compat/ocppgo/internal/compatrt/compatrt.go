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
	"crypto/tls"
	"net"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/shiv3/gocpp/core/dispatcher"
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
}

// Conn is what a runtime gets for an accepted WebSocket connection.
type Conn struct {
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
}
