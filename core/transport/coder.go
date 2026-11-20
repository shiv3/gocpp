package transport

import (
	"context"

	"github.com/coder/websocket"
)

type coderWS struct {
	c *websocket.Conn
}

// DefaultMaxMessageSize is the default inbound message size limit (1 MiB).
const DefaultMaxMessageSize int64 = 1 << 20

// NewCoderWS wraps a coder/websocket connection as a WS with the default
// inbound message size limit.
func NewCoderWS(c *websocket.Conn) WS {
	return NewCoderWSWithLimit(c, DefaultMaxMessageSize)
}

// NewCoderWSWithLimit is NewCoderWS with an explicit inbound message size limit
// in bytes; a message larger than that closes the connection with status 1009.
// limit <= 0 selects DefaultMaxMessageSize. OCPP-J messages can be large
// (firmware, certificates), so the default is above coder's own 32 KiB.
func NewCoderWSWithLimit(c *websocket.Conn, limit int64) WS {
	if limit <= 0 {
		limit = DefaultMaxMessageSize
	}
	c.SetReadLimit(limit)
	return &coderWS{c: c}
}

func (w *coderWS) Read(ctx context.Context) ([]byte, error) {
	_, data, err := w.c.Read(ctx)
	return data, err
}

func (w *coderWS) Write(ctx context.Context, data []byte) error {
	return w.c.Write(ctx, websocket.MessageText, data)
}

func (w *coderWS) Ping(ctx context.Context) error {
	return w.c.Ping(ctx)
}

func (w *coderWS) Close(code StatusCode, reason string) error {
	return w.c.Close(websocket.StatusCode(code), reason)
}

// Abort drops the connection without a close handshake (coder's CloseNow).
func (w *coderWS) Abort() error { return w.c.CloseNow() }

func (w *coderWS) Subprotocol() string {
	return w.c.Subprotocol()
}
