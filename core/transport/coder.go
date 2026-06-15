package transport

import (
	"context"

	"github.com/coder/websocket"
)

type coderWS struct {
	c *websocket.Conn
}

// NewCoderWS wraps a coder/websocket connection as a WS.
func NewCoderWS(c *websocket.Conn) WS {
	// OCPP-J messages can be large (firmware, certificates). Lift the read limit.
	c.SetReadLimit(1 << 20) // 1 MiB
	return &coderWS{c: c}
}

func (w *coderWS) Read(ctx context.Context) ([]byte, error) {
	_, data, err := w.c.Read(ctx)
	return data, err
}

func (w *coderWS) Write(ctx context.Context, data []byte) error {
	return w.c.Write(ctx, websocket.MessageText, data)
}

func (w *coderWS) Close(code StatusCode, reason string) error {
	return w.c.Close(websocket.StatusCode(code), reason)
}

func (w *coderWS) Subprotocol() string {
	return w.c.Subprotocol()
}
