package ocppj

import (
	"encoding/json"
	"fmt"
)

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
	case MessageTypeCallError:
		if len(arr) != 5 {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "call error must have 5 elements"}
		}
		var code, desc string
		_ = json.Unmarshal(arr[2], &code)
		_ = json.Unmarshal(arr[3], &desc)
		return Frame{Type: MessageTypeCallError, MsgID: msgID, ErrCode: code, ErrDesc: desc, ErrData: arr[4]}, nil
	case Send:
		if len(arr) != 4 {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "send must have 4 elements"}
		}
		var action string
		if err := json.Unmarshal(arr[2], &action); err != nil {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "action not a string"}
		}
		return Frame{Type: Send, MsgID: msgID, Action: action, Payload: arr[3]}, nil
	case MessageTypeCallResultError:
		if len(arr) != 5 {
			return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "call result error must have 5 elements"}
		}
		var code, desc string
		_ = json.Unmarshal(arr[2], &code)
		_ = json.Unmarshal(arr[3], &desc)
		return Frame{Type: MessageTypeCallResultError, MsgID: msgID, ErrCode: code, ErrDesc: desc, ErrData: arr[4]}, nil
	default:
		return Frame{}, fmt.Errorf("%w: %d", ErrIgnoredMessageType, mt)
	}
}
