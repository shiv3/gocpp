// Package datatransfer provides typed helpers for the free-form DataTransfer
// payload.
//
// The two schema families differ. OCPP 1.6 declares data as a string, so the
// generated field is *string and a vendor payload travels JSON-encoded inside
// that string: use MarshalString / UnmarshalString. OCPP 2.0.1 and 2.1 declare
// data without a type (any JSON value), so the generated field is
// json.RawMessage and the payload is embedded directly: use Marshal / Unmarshal.
package datatransfer

import "encoding/json"

// Marshal JSON-encodes v into a json.RawMessage suitable for the Data field of
// an OCPP 2.x DataTransfer request or response.
func Marshal[T any](v T) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return b, nil
}

// Unmarshal JSON-decodes an OCPP 2.x DataTransfer Data field into T. An absent
// (nil or empty) data yields the zero value of T and no error.
func Unmarshal[T any](data json.RawMessage) (T, error) {
	var v T
	if len(data) == 0 {
		return v, nil
	}
	err := json.Unmarshal(data, &v)
	return v, err
}

// MarshalString JSON-encodes v into a *string suitable for the Data field of an
// OCPP 1.6 DataTransfer request or response, where data is a string.
func MarshalString[T any](v T) (*string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// UnmarshalString JSON-decodes an OCPP 1.6 DataTransfer Data string into T. A
// nil or empty data pointer yields the zero value of T and no error.
func UnmarshalString[T any](data *string) (T, error) {
	var v T
	if data == nil || *data == "" {
		return v, nil
	}
	err := json.Unmarshal([]byte(*data), &v)
	return v, err
}
