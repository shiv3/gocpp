package ocppj

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

const maxRawInError = 256

// MaxMessageIDLength is the OCPP-J limit on a message id, in characters.
const MaxMessageIDLength = 36

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

// parseSlow is the reflective reference implementation. Parse falls back to it
// for every input its scanner does not handle, so it stays the only producer of
// errors and of the unusual-but-accepted shapes (escaped strings, non-string
// error codes, unknown message types, ...).
func parseSlow(raw []byte) (Frame, error) {
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return Frame{}, &ProtocolError{Stage: "parse", Raw: truncateRaw(raw), Message: "not a JSON array: " + err.Error()}
	}
	if len(arr) < 2 {
		return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "array too short", Elements: len(arr)}
	}
	var mt int
	if err := json.Unmarshal(arr[0], &mt); err != nil {
		return Frame{}, &ProtocolError{Stage: "type", Raw: truncateRaw(raw), Message: "message type not an int"}
	}
	var msgID string
	if err := json.Unmarshal(arr[1], &msgID); err != nil {
		return Frame{}, &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: "message id not a string"}
	}

	shapeErr := func(msg string) *ProtocolError {
		return &ProtocolError{Stage: "shape", Raw: truncateRaw(raw), Message: msg, MessageType: mt, MsgID: msgID, Elements: len(arr)}
	}

	switch MessageType(mt) {
	case Call:
		if len(arr) != 4 {
			return Frame{}, shapeErr("call must have 4 elements")
		}
		var action string
		if err := json.Unmarshal(arr[2], &action); err != nil {
			pe := shapeErr("action not a string")
			pe.BadField = "action"
			return Frame{}, pe
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
			pe := shapeErr("action not a string")
			pe.BadField = "action"
			return Frame{}, pe
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
		return Frame{}, &UnknownMessageTypeError{MessageType: mt, MsgID: msgID}
	}
}

// Parse decodes a raw OCPP-J message into a Frame.
//
// Common, well-formed frames go through a hand-written scanner that allocates only
// the message id and action strings. Payload and ErrData alias raw (they are
// sub-slices, not copies), so the caller must not modify raw while it uses the
// Frame; every reader in gocpp hands over a buffer it no longer touches. Anything
// the scanner does not recognise, including every malformed frame, is decoded by
// parseSlow, which yields exactly the Frame or error the reflective decoder
// always produced (FuzzParseEquivalence checks this).
func Parse(raw []byte) (Frame, error) {
	if f, ok := parseFast(raw); ok {
		return f, nil
	}
	return parseSlow(raw)
}

func isJSONSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\r' || c == '\n' }

func skipSpace(b []byte, i int) int {
	for i < len(b) && isJSONSpace(b[i]) {
		i++
	}
	return i
}

// skipValue returns the index just past the JSON value starting at b[i]. b must
// already be valid JSON (json.Valid), which is why it only tracks nesting and
// strings.
func skipValue(b []byte, i int) int {
	switch b[i] {
	case '"':
		return skipString(b, i)
	case '{', '[':
		depth := 0
		for i < len(b) {
			switch b[i] {
			case '"':
				i = skipString(b, i)
				continue
			case '{', '[':
				depth++
			case '}', ']':
				depth--
				if depth == 0 {
					return i + 1
				}
			}
			i++
		}
		return i
	default: // number, true, false, null
		for i < len(b) {
			c := b[i]
			if c == ',' || c == ']' || c == '}' || isJSONSpace(c) {
				break
			}
			i++
		}
		return i
	}
}

func skipString(b []byte, i int) int {
	i++ // opening quote
	for i < len(b) {
		switch b[i] {
		case '\\':
			i += 2
		case '"':
			return i + 1
		default:
			i++
		}
	}
	return i
}

// plainString returns the content of the JSON string token b (quotes included)
// when it contains no escape sequence and is valid UTF-8, so it equals what
// json.Unmarshal would produce.
func plainString(b []byte) (string, bool) {
	if len(b) < 2 || b[0] != '"' {
		return "", false
	}
	s := b[1 : len(b)-1]
	for _, c := range s {
		if c == '\\' {
			return "", false
		}
	}
	if !utf8.Valid(s) {
		return "", false
	}
	return string(s), true
}

const maxFastElems = 5

func parseFast(raw []byte) (Frame, bool) {
	if !json.Valid(raw) {
		return Frame{}, false
	}
	i := skipSpace(raw, 0)
	if i >= len(raw) || raw[i] != '[' {
		return Frame{}, false
	}
	i++
	var el [maxFastElems][]byte
	n := 0
	for {
		i = skipSpace(raw, i)
		if i >= len(raw) {
			return Frame{}, false
		}
		if raw[i] == ']' { // only an empty array can have ] here: Valid rules out a trailing comma
			break
		}
		if n == maxFastElems {
			return Frame{}, false // longer than any shape we fast-path
		}
		end := skipValue(raw, i)
		el[n] = raw[i:end]
		n++
		i = skipSpace(raw, end)
		if i < len(raw) && raw[i] == ',' {
			i++
		}
	}
	if n < 3 || len(el[0]) != 1 {
		return Frame{}, false
	}
	mt := MessageType(el[0][0] - '0')
	msgID, ok := plainString(el[1])
	if !ok {
		return Frame{}, false
	}
	switch mt {
	case Call, Send:
		if n != 4 {
			return Frame{}, false
		}
		action, ok := plainString(el[2])
		if !ok {
			return Frame{}, false
		}
		base, signed := splitSignedAction(action)
		return Frame{Type: mt, Signed: signed, MsgID: msgID, Action: base, Payload: el[3]}, true
	case CallResult:
		return Frame{Type: CallResult, MsgID: msgID, Payload: el[2]}, true
	case MessageTypeCallError, MessageTypeCallResultError:
		if n < 4 {
			return Frame{}, false
		}
		code, ok1 := plainString(el[2])
		desc, ok2 := plainString(el[3])
		if !ok1 || !ok2 {
			return Frame{}, false
		}
		var data []byte
		if n > 4 {
			data = el[4]
		}
		return Frame{Type: mt, MsgID: msgID, ErrCode: code, ErrDesc: desc, ErrData: data}, true
	}
	return Frame{}, false
}
