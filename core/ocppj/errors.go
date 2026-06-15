package ocppj

import (
	"errors"
	"fmt"
)

// ProtocolError indicates an OCPP-J frame that does not conform to the wire format.
type ProtocolError struct {
	Stage   string // "parse", "type", "shape"
	Raw     string
	Message string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("ocppj: protocol error at %s: %s", e.Stage, e.Message)
}

var (
	ErrConnClosed       = errors.New("ocpp: connection closed")
	ErrConnDropped      = errors.New("ocpp: connection dropped")
	ErrNotConnected     = errors.New("ocpp: not connected")
	ErrAlreadyConnected = errors.New("ocpp: already connected")

	ErrCallTimeout         = errors.New("ocpp: call timeout")
	ErrCallCancelled       = errors.New("ocpp: call cancelled")
	ErrQueueFull           = errors.New("ocpp: outbound queue full")
	ErrConcurrentCallLimit = errors.New("ocpp: concurrent call limit exceeded")

	ErrUnknownAction        = errors.New("ocpp: unknown action")
	ErrHandlerNotRegistered = errors.New("ocpp: handler not registered")
	ErrInvalidDirection     = errors.New("ocpp: invalid message direction")
	ErrDuplicateHandler     = errors.New("ocpp: handler already registered")

	ErrUnsupportedVersion = errors.New("ocpp: unsupported version")
	ErrVersionMismatch    = errors.New("ocpp: version mismatch on connection")
)
