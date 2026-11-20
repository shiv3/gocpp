package ocppj

import (
	"bytes"
	"encoding/json"
)

// encodeArray is the reference encoder (boxing every element, json.Marshal per
// element). The Encode* functions use appendFrame, which writes the same bytes;
// FuzzEncodeEquivalence compares the two.
func encodeArray(elems ...any) ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, e := range elems {
		if i > 0 {
			buf.WriteByte(',')
		}
		switch v := e.(type) {
		case json.RawMessage:
			buf.Write(v)
		case []byte:
			buf.Write(v)
		default:
			b, err := json.Marshal(v)
			if err != nil {
				return nil, err
			}
			buf.Write(b)
		}
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// appendJSONString appends s as a JSON string with the bytes encoding/json would
// produce. Printable ASCII without '"', '\\', '<', '>' or '&' (the characters
// json.Marshal escapes there) is copied directly; any other string, including
// every non-ASCII one, goes through json.Marshal.
func appendJSONString(dst []byte, s string) []byte {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' || c == '<' || c == '>' || c == '&' {
			b, _ := json.Marshal(s) // cannot fail for a string
			return append(dst, b...)
		}
	}
	dst = append(dst, '"')
	dst = append(dst, s...)
	return append(dst, '"')
}

// appendFrame builds [type,"msgID",("a","b",)...payload] into one buffer sized for
// the common case. strs are the string elements between the id and the raw
// trailing element (action, or error code and description); raw is appended as is.
func appendFrame(mt MessageType, msgID string, strs [2]string, nstr int, raw []byte) []byte {
	n := 8 + len(msgID) + len(raw)
	for _, s := range strs[:nstr] {
		n += len(s) + 3
	}
	dst := make([]byte, 0, n)
	dst = append(dst, '[', byte('0'+int(mt)), ',')
	dst = appendJSONString(dst, msgID)
	for _, s := range strs[:nstr] {
		dst = append(dst, ',')
		dst = appendJSONString(dst, s)
	}
	dst = append(dst, ',')
	dst = append(dst, raw...)
	return append(dst, ']')
}

// EncodeCall builds a [2, msgID, action, payload] frame.
func EncodeCall(msgID, action string, payload []byte) ([]byte, error) {
	return appendFrame(Call, msgID, [2]string{action}, 1, payload), nil
}

// EncodeCallResult builds a [3, msgID, payload] frame.
func EncodeCallResult(msgID string, payload []byte) ([]byte, error) {
	return appendFrame(CallResult, msgID, [2]string{}, 0, payload), nil
}

// EncodeCallError builds a [4, msgID, code, desc, details] frame.
func EncodeCallError(msgID, code, desc string, details []byte) ([]byte, error) {
	if len(details) == 0 {
		details = []byte("{}")
	}
	return appendFrame(MessageTypeCallError, msgID, [2]string{code, desc}, 2, details), nil
}

// EncodeSend builds a [6, msgID, action, payload] frame (OCPP 2.1 SEND).
func EncodeSend(msgID, action string, payload []byte) ([]byte, error) {
	return appendFrame(Send, msgID, [2]string{action}, 1, payload), nil
}

// EncodeCallResultError builds a [5, msgID, code, desc, details] frame (OCPP 2.1
// CALLRESULTERROR).
func EncodeCallResultError(msgID, code, desc string, details []byte) ([]byte, error) {
	if len(details) == 0 {
		details = []byte("{}")
	}
	return appendFrame(MessageTypeCallResultError, msgID, [2]string{code, desc}, 2, details), nil
}

// EncodeSignedCall builds a [2, msgID, action+"-Signed", signedPayload] frame
// (OCPP 2.1 Part 4 Ch 7). signedPayload is a Flattened JWS JSON object.
func EncodeSignedCall(msgID, action string, signedPayload []byte) ([]byte, error) {
	return appendFrame(Call, msgID, [2]string{action + signedSuffix}, 1, signedPayload), nil
}

// EncodeSignedSend builds a [6, msgID, action+"-Signed", signedPayload] frame.
func EncodeSignedSend(msgID, action string, signedPayload []byte) ([]byte, error) {
	return appendFrame(Send, msgID, [2]string{action + signedSuffix}, 1, signedPayload), nil
}
