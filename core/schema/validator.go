// Package schema provides JSON Schema validation backed by santhosh-tekuri/jsonschema.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Validator validates raw JSON against a compiled OCPP schema.
type Validator struct {
	schema *jsonschema.Schema
	name   string
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
	return &Validator{schema: s, name: name}, nil
}

// Validate checks raw JSON against the schema.
func (v *Validator) Validate(raw []byte) error {
	var inst any
	if err := json.NewDecoder(bytes.NewReader(raw)).Decode(&inst); err != nil {
		return fmt.Errorf("decode instance: %w", err)
	}
	if err := v.schema.Validate(inst); err != nil {
		return fmt.Errorf("schema %s: %w", v.name, err)
	}
	return nil
}
