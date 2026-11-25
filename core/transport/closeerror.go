package transport

import (
	"errors"
	"fmt"

	"github.com/coder/websocket"
)

// CloseError describes why a WebSocket connection ended: the close code and
// reason in the close frame. AsCloseError extracts it from the error a
// connection ends with (for example the argument of cp.WithOnDisconnect or
// csms.WithOnDisconnect).
type CloseError struct {
	Code   StatusCode
	Reason string
}

func (e *CloseError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("websocket closed: status %d", int(e.Code))
	}
	return fmt.Sprintf("websocket closed: status %d (%s)", int(e.Code), e.Reason)
}

// Normal reports whether the close was an orderly one that needs no
// investigation: 1000 (normal closure), 1001 (going away) or 1005 (no status
// received, a close frame without a code). Everything else, a protocol error,
// policy violation, too big message, 1006 or a server restart, is not normal.
func (e *CloseError) Normal() bool {
	switch e.Code {
	case StatusNormalClosure, StatusGoingAway, StatusNoStatusReceived:
		return true
	}
	return false
}

// StatusNoStatusReceived (1005) is reported when a close frame carried no code.
const StatusNoStatusReceived StatusCode = 1005

// closeCoder is implemented by close causes of this module that know the code
// they send (dispatcher.CloseStatus).
type closeCoder interface {
	CloseCode() (StatusCode, string)
}

// AsCloseError finds a WebSocket close in err's chain: a close frame received
// from the peer, or a close this module sent with a chosen code (a connection
// closed by a limit, replacement or drain). ok is false when the connection
// ended without a close frame (a network error, a timeout).
func AsCloseError(err error) (*CloseError, bool) {
	var cc closeCoder
	if errors.As(err, &cc) {
		code, reason := cc.CloseCode()
		return &CloseError{Code: code, Reason: reason}, true
	}
	var ce websocket.CloseError
	if errors.As(err, &ce) {
		return &CloseError{Code: StatusCode(ce.Code), Reason: ce.Reason}, true
	}
	return nil, false
}
