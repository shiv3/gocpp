package transport

import "github.com/coder/websocket"

// CompressionMode selects RFC 7692 permessage-deflate behavior for a WebSocket
// connection. It mirrors coder/websocket's modes so callers do not import that
// package directly.
type CompressionMode int

const (
	// CompressionDisabled turns permessage-deflate off (no negotiation).
	CompressionDisabled CompressionMode = iota
	// CompressionContextTakeover keeps the flate sliding window across messages
	// (best ratio, more memory per connection).
	CompressionContextTakeover
	// CompressionNoContextTakeover resets the flate window per message (less
	// memory, slightly worse ratio). Default for OCPP connections.
	CompressionNoContextTakeover
)

// Coder maps to the coder/websocket CompressionMode.
func (m CompressionMode) Coder() websocket.CompressionMode {
	switch m {
	case CompressionContextTakeover:
		return websocket.CompressionContextTakeover
	case CompressionNoContextTakeover:
		return websocket.CompressionNoContextTakeover
	default:
		return websocket.CompressionDisabled
	}
}
