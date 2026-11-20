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
	// MessageType and MsgID are filled when they could be decoded before the
	// frame was found malformed (zero/empty otherwise). They let the receiver
	// answer a malformed CALL with a CallError for the right message id.
	MessageType int
	MsgID       string
	// Elements is the number of elements of the frame's JSON array (0 when the
	// frame was not an array). BadField names the element that failed to decode
	// ("action" for a non-string action), empty otherwise.
	Elements int
	BadField string
}

func (e *ProtocolError) Error() string {
	return fmt.Sprintf("ocppj: protocol error at %s: %s", e.Stage, e.Message)
}

var (
	ErrConnClosed = errors.New("ocpp: connection closed")
	// ErrConnClosedByServer is the close cause of a connection the CSMS closed on
	// purpose through csms.Conn.Close or csms.Server.Disconnect. It is what the
	// disconnect callback receives and what pending calls fail with, so an
	// application can tell an administrative close from a network drop
	// (ErrConnDropped / a read error) or a replacement (ErrConnReplaced).
	ErrConnClosedByServer = errors.New("ocpp: connection closed by the server")
	ErrConnDropped        = errors.New("ocpp: connection dropped")
	// ErrConnReplaced is the close cause of a connection that was superseded by a
	// newer connection with the same charge point id.
	ErrConnReplaced     = errors.New("ocpp: connection replaced by a newer connection with the same id")
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

	// ErrIgnoredMessageType is returned by Parse for a well-formed frame whose
	// MessageTypeId is not one this implementation handles. Per OCPP 2.1 Part 4
	// §4.4 the payload is ignored; the reader drops it without warning.
	ErrIgnoredMessageType = errors.New("ocpp: ignored message type")
)

// UnknownMessageTypeError is returned by Parse for a well-formed frame (a JSON
// array with a readable message id) whose MessageTypeId is not one this
// implementation handles. It matches ErrIgnoredMessageType under errors.Is.
// OCPP 2.0.1 / 2.1 receivers answer it with a CALLERROR MessageTypeNotSupported
// (Part 4 §4.2.3 error code table) using MsgID; OCPP 1.6J has no such code.
type UnknownMessageTypeError struct {
	MessageType int
	MsgID       string
}

func (e *UnknownMessageTypeError) Error() string {
	return fmt.Sprintf("%s: %d", ErrIgnoredMessageType.Error(), e.MessageType)
}

func (e *UnknownMessageTypeError) Unwrap() error { return ErrIgnoredMessageType }
