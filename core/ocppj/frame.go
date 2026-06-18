// Package ocppj implements OCPP-J (JSON over WebSocket) framing.
package ocppj

import "fmt"

// MessageType is the OCPP-J MessageTypeId (first array element).
type MessageType int

const (
	Call                       MessageType = 2
	CallResult                 MessageType = 3
	MessageTypeCallError       MessageType = 4
	MessageTypeCallResultError MessageType = 5 // OCPP 2.1
	Send                       MessageType = 6 // OCPP 2.1
)

func (m MessageType) String() string {
	switch m {
	case Call:
		return "Call"
	case CallResult:
		return "CallResult"
	case MessageTypeCallError:
		return "CallError"
	case MessageTypeCallResultError:
		return "CallResultError"
	case Send:
		return "Send"
	default:
		return fmt.Sprintf("Unknown(%d)", int(m))
	}
}

// Frame is a parsed OCPP-J message.
type Frame struct {
	Type    MessageType
	MsgID   string
	Action  string // Call / Send
	Payload []byte // Call / CallResult / Send: raw JSON object
	ErrCode string // CallError / CallResultError
	ErrDesc string // CallError / CallResultError
	ErrData []byte // CallError / CallResultError: raw JSON object
}
