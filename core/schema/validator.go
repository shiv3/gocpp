// Package schema provides JSON Schema validation backed by santhosh-tekuri/jsonschema.
package schema

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates raw JSON against a compiled OCPP schema.
type Validator struct {
	schema *jsonschema.Schema
	name   string
	// doc is the raw schema document; the lenient coercion pass walks it to
	// learn each field's declared type and format.
	doc any
}

// New compiles the named schema file from the given filesystem.
func New(fsys fs.FS, name string) (*Validator, error) {
	data, err := fs.ReadFile(fsys, name)
	if err != nil {
		return nil, fmt.Errorf("read schema %s: %w", name, err)
	}
	var doc any
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse schema %s: %w", name, err)
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource(name, doc); err != nil {
		return nil, fmt.Errorf("add schema %s: %w", name, err)
	}
	s, err := c.Compile(name)
	if err != nil {
		return nil, fmt.Errorf("compile schema %s: %w", name, err)
	}
	return &Validator{schema: s, name: name, doc: doc}, nil
}

// Validate checks raw JSON against the schema.
func (v *Validator) Validate(raw []byte) error {
	var inst any
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&inst); err != nil {
		return fmt.Errorf("decode instance: %w", err)
	}
	if err := v.schema.Validate(inst); err != nil {
		return newViolation(fmt.Errorf("schema %s: %w", v.name, err), err)
	}
	return nil
}

// ViolationError is a schema rejection that records the JSON-Schema keyword of
// the first violation, so callers can pick the matching OCPP-J CALLERROR code.
// Its message is the underlying validator message; errors.As still reaches the
// validator's own error through Unwrap.
type ViolationError struct {
	// Keyword is the failing keyword of the first leaf violation ("required",
	// "type", "enum", "maxLength", ...); empty when it could not be determined.
	Keyword string
	// AtRoot reports that the violation is on the payload itself rather than on
	// one of its properties (for example a payload that is not a JSON object).
	AtRoot bool
	err    error
}

func (e *ViolationError) Error() string { return e.err.Error() }
func (e *ViolationError) Unwrap() error { return e.err }

// newViolation wraps wrapped in a ViolationError carrying the keyword of the
// first leaf of verr (when verr is a validator error).
func newViolation(wrapped, verr error) error {
	ve := &ViolationError{err: wrapped}
	var jve *jsonschema.ValidationError
	if errors.As(verr, &jve) {
		var leaves []*jsonschema.ValidationError
		collectLeaves(jve, &leaves)
		if len(leaves) > 0 {
			ve.Keyword = keywordOf(leaves[0])
			ve.AtRoot = len(leaves[0].InstanceLocation) == 0
		}
	}
	return ve
}
