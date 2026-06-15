package ocppj

import (
	"bytes"
	"encoding/json"
)

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

// EncodeCall builds a [2, msgID, action, payload] frame.
func EncodeCall(msgID, action string, payload []byte) ([]byte, error) {
	return encodeArray(int(Call), msgID, action, json.RawMessage(payload))
}

// EncodeCallResult builds a [3, msgID, payload] frame.
func EncodeCallResult(msgID string, payload []byte) ([]byte, error) {
	return encodeArray(int(CallResult), msgID, json.RawMessage(payload))
}

// EncodeCallError builds a [4, msgID, code, desc, details] frame.
func EncodeCallError(msgID, code, desc string, details []byte) ([]byte, error) {
	if len(details) == 0 {
		details = []byte("{}")
	}
	return encodeArray(int(CallError), msgID, code, desc, json.RawMessage(details))
}
