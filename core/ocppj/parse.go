package ocppj

import (
	"encoding/json"
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

const maxRawInError = 256

func truncateRaw(raw []byte) string {
	if len(raw) > maxRawInError {
		return string(raw[:maxRawInError])
	}
	return string(raw)
}

// Parse decodes a raw OCPP-J message into a Frame.
func Parse(raw []byte) (Frame, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return Frame{}, &ProtocolError{Stage: "parse", Raw: truncateRaw(raw), Message: "not a JSON array: " + err.Error()}
	}
	if len(arr) < 2 {
		return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "array too short"}
	}
	var mt int
	if err := json.Unmarshal(arr[0], &mt); err != nil {
		return Frame{}, &ProtocolError{Stage: "type", Raw: truncateRaw(raw), Message: "message type not an int"}
	}
	var msgID string
	if err := json.Unmarshal(arr[1], &msgID); err != nil {
		return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "message id not a string"}
	}

	switch MessageType(mt) {
	case Call:
		if len(arr) != 4 {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "call must have 4 elements"}
		}
		var action string
		if err := json.Unmarshal(arr[2], &action); err != nil {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "action not a string"}
		}
		return Frame{Type: Call, MsgID: msgID, Action: action, Payload: arr[3]}, nil
	case CallResult:
		if len(arr) != 3 {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "call result must have 3 elements"}
		}
		return Frame{Type: CallResult, MsgID: msgID, Payload: arr[2]}, nil
	case CallError:
		if len(arr) != 5 {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "call error must have 5 elements"}
		}
		var code, desc string
		_ = json.Unmarshal(arr[2], &code)
		_ = json.Unmarshal(arr[3], &desc)
		return Frame{Type: CallError, MsgID: msgID, ErrCode: code, ErrDesc: desc, ErrData: arr[4]}, nil
	default:
		return Frame{}, &ProtocolError{Stage: "type", Raw: truncateRaw(raw), Message: fmt.Sprintf("unknown message type %d", mt)}
	}
}
