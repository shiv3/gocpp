// Package transport abstracts the WebSocket layer so the dispatcher never
// touches a concrete websocket library.
package transport

import "context"

// WS is a context-native WebSocket connection. All blocking operations accept a
// context so the dispatcher can cancel reads/writes during connection teardown.
//
// Concurrency contract: the dispatcher calls Read from one goroutine at a time
// and serializes Write calls itself (one write in flight), but Ping and Close may
// run concurrently with Read and Write, and Close must unblock a Write that is in
// flight. The slice returned by Read is owned by the caller.
type WS interface {
	// Read blocks until one full text message arrives or ctx is cancelled.
	Read(ctx context.Context) ([]byte, error)
	// Write sends one text message, respecting ctx for cancellation.
	Write(ctx context.Context, data []byte) error
	// Ping sends a WebSocket ping and waits for the matching pong.
	Ping(ctx context.Context) error
	// Close sends a close frame with the given status code and reason.
	Close(code StatusCode, reason string) error
	// Subprotocol returns the negotiated subprotocol (e.g. "ocpp1.6").
	Subprotocol() string
}

// Aborter is an optional interface of a WS: Abort closes the underlying
// connection at once, without a close handshake, and unblocks any Read or Write
// in flight. When a transport implements it the dispatcher enforces WriteTimeout
// with one lazily armed timer per connection instead of a context per write.
// Abort may be called concurrently with Write and more than once.
type Aborter interface {
	Abort() error
}

// StatusCode mirrors RFC 6455 close codes (subset used by OCPP).
type StatusCode int

const (
	StatusNormalClosure   StatusCode = 1000
	StatusGoingAway       StatusCode = 1001
	StatusProtocolError   StatusCode = 1002
	StatusAbnormalClosure StatusCode = 1006
	StatusPolicyViolation StatusCode = 1008
	StatusInternalError   StatusCode = 1011
)
