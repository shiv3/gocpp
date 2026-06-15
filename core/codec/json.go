// Package codec is the internal JSON seam. It currently delegates to encoding/json;
// a faster library (sonic/go-json) can be swapped here behind a build tag if
// profiling justifies it (spec OQ-04). Hot-path call sites use this package rather
// than encoding/json directly so the implementation can change in one place.
package codec

import "encoding/json"

// Marshal serializes v to JSON.
func Marshal(v any) ([]byte, error) { return json.Marshal(v) }

// Unmarshal deserializes data into v.
func Unmarshal(data []byte, v any) error { return json.Unmarshal(data, v) }
