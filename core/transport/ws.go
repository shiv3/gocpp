// Package transport abstracts the WebSocket layer so the dispatcher never
// touches a concrete websocket library.
package transport

import "context"

// WS is a context-native WebSocket connection. All blocking operations accept a
// context so the dispatcher can cancel reads/writes during connection teardown.
type WS interface {
	// Read blocks until one full text message arrives or ctx is cancelled.
	Read(ctx context.Context) ([]byte, error)
	// Write sends one text message, respecting ctx for cancellation.
	Write(ctx context.Context, data []byte) error
	// Close sends a close frame with the given status code and reason.
	Close(code StatusCode, reason string) error
	// Subprotocol returns the negotiated subprotocol (e.g. "ocpp1.6").
	Subprotocol() string
}

// StatusCode mirrors RFC 6455 close codes (subset used by OCPP).
type StatusCode int

const (
	StatusNormalClosure StatusCode = 1000
	StatusGoingAway     StatusCode = 1001
	StatusProtocolError StatusCode = 1002
	StatusInternalError StatusCode = 1011
)
