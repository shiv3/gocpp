package ocppj

import (
	"encoding/json"
	"fmt"
	"strings"
)

const maxRawInError = 256

// signedSuffix is the OCPP 2.1 Part 4 §7.1 marker appended to a signed message's
// action name (e.g. "BootNotification-Signed").
const signedSuffix = "-Signed"

// splitSignedAction reports whether action carries the "-Signed" suffix and
// returns the base action with the suffix removed.
func splitSignedAction(action string) (base string, signed bool) {
	if b, ok := strings.CutSuffix(action, signedSuffix); ok {
		return b, true
	}
	return action, false
}

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

	shapeErr := func(msg string) error {
		return &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: msg, MessageType: mt, MsgID: msgID}
	}

	switch MessageType(mt) {
	case Call:
		if len(arr) != 4 {
			return Frame{}, shapeErr("call must have 4 elements")
		}
		var action string
		if err := json.Unmarshal(arr[2], &action); err != nil {
			return Frame{}, shapeErr("action not a string")
		}
		base, signed := splitSignedAction(action)
		return Frame{Type: Call, Signed: signed, MsgID: msgID, Action: base, Payload: arr[3]}, nil
	case CallResult:
		// Extra trailing elements are ignored.
		if len(arr) < 3 {
			return Frame{}, shapeErr("call result must have at least 3 elements")
		}
		return Frame{Type: CallResult, MsgID: msgID, Payload: arr[2]}, nil
	case MessageTypeCallError:
		// Details are optional; a non-string code or description reads as "".
		if len(arr) < 4 {
			return Frame{}, shapeErr("call error must have at least 4 elements")
		}
		var code, desc string
		_ = json.Unmarshal(arr[2], &code)
		_ = json.Unmarshal(arr[3], &desc)
		var data []byte
		if len(arr) > 4 {
			data = arr[4]
		}
		return Frame{Type: MessageTypeCallError, MsgID: msgID, ErrCode: code, ErrDesc: desc, ErrData: data}, nil
	case Send:
		if len(arr) != 4 {
			return Frame{}, shapeErr("send must have 4 elements")
		}
		var action string
		if err := json.Unmarshal(arr[2], &action); err != nil {
			return Frame{}, shapeErr("action not a string")
		}
		base, signed := splitSignedAction(action)
		return Frame{Type: Send, Signed: signed, MsgID: msgID, Action: base, Payload: arr[3]}, nil
	case MessageTypeCallResultError:
		// Details are optional; a non-string code or description reads as "".
		if len(arr) < 4 {
			return Frame{}, shapeErr("call result error must have at least 4 elements")
		}
		var code, desc string
		_ = json.Unmarshal(arr[2], &code)
		_ = json.Unmarshal(arr[3], &desc)
		var data []byte
		if len(arr) > 4 {
			data = arr[4]
		}
		return Frame{Type: MessageTypeCallResultError, MsgID: msgID, ErrCode: code, ErrDesc: desc, ErrData: data}, nil
	default:
		return Frame{}, fmt.Errorf("%w: %d", ErrIgnoredMessageType, mt)
	}
}
