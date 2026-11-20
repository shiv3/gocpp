package types

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// MarshalWithExtra encodes v (a struct value, normally a local alias of a
// generated type that has no MarshalJSON of its own) and merges extra into the
// resulting JSON object. Properties already produced by v win over keys of the
// same name in extra. It exists for schema types that allow additional
// properties, such as the 2.x CustomDataType.
func MarshalWithExtra(v any, extra map[string]json.RawMessage) ([]byte, error) {
	base, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	if len(extra) == 0 {
		return base, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(base, &fields); err != nil {
		return nil, fmt.Errorf("merge extra properties: %w", err)
	}
	for k, raw := range extra {
		if _, defined := fields[k]; !defined {
			fields[k] = raw
		}
	}
	// encoding/json sorts map keys, so the output is deterministic.
	return json.Marshal(fields)
}

// UnmarshalWithExtra decodes data into v (a pointer to a local alias of the
// generated type, so UnmarshalJSON does not recurse) and returns every
// property whose name is not in known. The result is nil when there are none.
func UnmarshalWithExtra(data []byte, v any, known ...string) (map[string]json.RawMessage, error) {
	if err := json.Unmarshal(data, v); err != nil {
		return nil, err
	}
	// A JSON null leaves v untouched, matching encoding/json's own behaviour.
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		return nil, nil
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(data, &all); err != nil {
		return nil, err
	}
	for _, k := range known {
		delete(all, k)
	}
	if len(all) == 0 {
		return nil, nil
	}
	return all, nil
}
