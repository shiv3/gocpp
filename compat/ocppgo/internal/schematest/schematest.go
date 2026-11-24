// Package schematest checks that Go message types of this module marshal to and
// unmarshal from JSON that conforms to the OCA JSON schemas (the schemas/
// directory of the gocpp repository). It is used by tests only.
package schematest

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

func schemaPath(version, name string) string {
	_, file, _, _ := runtime.Caller(0)
	// <repo>/compat/ocppgo/internal/schematest/schematest.go
	root := filepath.Join(filepath.Dir(file), "..", "..", "..", "..")
	return filepath.Join(root, "schemas", version, name+".json")
}

func compile(t testing.TB, version, name string) *jsonschema.Schema {
	t.Helper()
	sch, err := jsonschema.NewCompiler().Compile(schemaPath(version, name))
	if err != nil {
		t.Fatalf("compile schema %s/%s: %v", version, name, err)
	}
	return sch
}

func decode(t testing.TB, data []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("decode %s: %v", data, err)
	}
	return v
}

// Valid fails unless data conforms to the schema (version "v16" or "v201", name
// such as "GetLogResponse").
func Valid(t testing.TB, version, name string, data []byte) {
	t.Helper()
	if err := compile(t, version, name).Validate(decode(t, data)); err != nil {
		t.Errorf("%s/%s: %s does not conform to the schema: %v", version, name, data, err)
	}
}

// Invalid fails if data conforms to the schema.
func Invalid(t testing.TB, version, name string, data []byte) {
	t.Helper()
	if err := compile(t, version, name).Validate(decode(t, data)); err == nil {
		t.Errorf("%s/%s: %s unexpectedly conforms to the schema", version, name, data)
	}
}

// Marshal marshals v and requires the result to conform to the schema.
func Marshal(t testing.TB, version, name string, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %T: %v", v, err)
	}
	Valid(t, version, name, data)
	return data
}

// RoundTrip takes a schema-valid sample, unmarshals it into a new value of the
// pointer type new() returns, runs validate (the module's struct validation) on
// it, marshals it again and requires the result to conform to the schema and to
// equal the sample (so no field is lost or invented).
func RoundTrip(t testing.TB, version, name, sample string, newPtr func() any, validate func(any) error) {
	t.Helper()
	Valid(t, version, name, []byte(sample))
	p := newPtr()
	if reflect.TypeOf(p).Kind() != reflect.Ptr {
		t.Fatalf("newPtr must return a pointer, got %T", p)
	}
	if err := json.Unmarshal([]byte(sample), p); err != nil {
		t.Fatalf("%s: unmarshal %s: %v", name, sample, err)
	}
	if validate != nil {
		if err := validate(p); err != nil {
			t.Errorf("%s: struct validation rejects a schema-valid message: %v", name, err)
		}
	}
	out := Marshal(t, version, name, p)
	if !reflect.DeepEqual(decode(t, []byte(sample)), decode(t, out)) {
		t.Errorf("%s: round trip changed the message\n in:  %s\n out: %s", name, sample, out)
	}
}
