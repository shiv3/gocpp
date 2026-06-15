// Package ocppj implements OCPP-J (JSON over WebSocket) framing.
package ocppj

import "fmt"

// MessageType is the OCPP-J MessageTypeId (first array element).
type MessageType int

const (
	Call       MessageType = 2
	CallResult MessageType = 3
	CallError  MessageType = 4
)

func (m MessageType) String() string {
	switch m {
	case Call:
		return "Call"
	case CallResult:
		return "CallResult"
	case CallError:
		return "CallError"
	default:
		return fmt.Sprintf("Unknown(%d)", int(m))
	}
}

// Frame is a parsed OCPP-J message.
type Frame struct {
	Type    MessageType
	MsgID   string
	Action  string // Call only
	Payload []byte // Call / CallResult: raw JSON object
	ErrCode string // CallError only
	ErrDesc string // CallError only
	ErrData []byte // CallError only: raw JSON object
}
