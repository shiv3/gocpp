// Package datatransfer provides typed helpers for the free-form DataTransfer
// payload, whose Data field is an optional JSON string (*string) in every OCPP
// version. Use these to encode/decode a vendor-specific payload type.
package datatransfer

import "encoding/json"

// Marshal JSON-encodes v into a *string suitable for a DataTransfer request or
// response Data field.
func Marshal[T any](v T) (*string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	s := string(b)
	return &s, nil
}

// Unmarshal JSON-decodes a DataTransfer Data field into T. A nil or empty data
// pointer yields the zero value of T and no error.
func Unmarshal[T any](data *string) (T, error) {
	var v T
	if data == nil || *data == "" {
		return v, nil
	}
	err := json.Unmarshal([]byte(*data), &v)
	return v, err
}
