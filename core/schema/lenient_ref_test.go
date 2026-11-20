package schema

// Reference implementation of the lenient path as it was before the stage 9
// rewrite (generic json.Decoder, closure-based coercion walk). The differential
// tests compare the production path against it; do not "optimize" this file.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

func refCoerceInstance(doc, inst any, raw []byte, opts LenientOptions, soft *[]string) bool {
	root, ok := doc.(map[string]any)
	if !ok {
		return false
	}
	c := &refCoercer{root: root, opts: opts, soft: soft, raw: raw}
	c.prim = c.coercePrimitive
	c.walk(root, inst, "", nil, 0)
	return c.changed
}

type refCoercer struct {
	root map[string]any
	opts LenientOptions
	soft *[]string
	raw  []byte
	// prim converts one primitive; it returns the new value, the soft keyword to
	// record (empty for none) and whether it changed anything.
	prim    func(typ, format string, v any) (any, string, bool)
	changed bool
}

const refMaxCoerceDepth = 64

// resolve follows local "#/definitions/X" references.
func (c *refCoercer) resolve(node map[string]any) map[string]any {
	for i := 0; i < 8; i++ {
		ref, ok := node["$ref"].(string)
		if !ok {
			return node
		}
		const prefix = "#/definitions/"
		if !strings.HasPrefix(ref, prefix) {
			return node
		}
		defs, _ := c.root["definitions"].(map[string]any)
		next, ok := defs[strings.TrimPrefix(ref, prefix)].(map[string]any)
		if !ok {
			return node
		}
		node = next
	}
	return node
}

// walk visits inst under schema node. key is the property name inst sits under
// ("" at the root and in arrays); set replaces inst in its parent.
func (c *refCoercer) walk(node map[string]any, inst any, key string, set func(any), depth int) {
	if depth > refMaxCoerceDepth {
		return
	}
	node = c.resolve(node)
	if c.softText(node, inst, key, set, depth) {
		return
	}
	switch v := inst.(type) {
	case map[string]any:
		props, _ := node["properties"].(map[string]any)
		for k, child := range v {
			cs, ok := props[k].(map[string]any)
			if !ok {
				continue
			}
			c.walk(cs, child, k, func(nv any) { v[k] = nv }, depth+1)
		}
	case []any:
		items, ok := node["items"].(map[string]any)
		if !ok {
			return
		}
		for i, child := range v {
			c.walk(items, child, "", func(nv any) { v[i] = nv }, depth+1)
		}
	default:
		if set == nil {
			return
		}
		typ, _ := node["type"].(string)
		format, _ := node["format"].(string)
		if nv, kw, ok := c.prim(typ, format, inst); ok {
			set(nv)
			c.changed = true
			if kw != "" && c.soft != nil {
				*c.soft = append(*c.soft, kw)
			}
		}
	}
}

// softText turns a JSON object, array or boolean found under a string-typed
// "data" property into its compact JSON text (OCPP 1.6 DataTransfer.data is a
// string in the schema but many stacks send structured JSON there). The text is
// lossless; at the payload root it is the original bytes, compacted. Only the
// property named "data" is converted: for any other string field a structured
// value is a real error. It reports whether it consumed inst.
func (c *refCoercer) softText(node map[string]any, inst any, key string, set func(any), depth int) bool {
	if c.soft == nil || key != "data" || set == nil {
		return false
	}
	if typ, _ := node["type"].(string); typ != "string" {
		return false
	}
	switch inst.(type) {
	case map[string]any, []any, bool:
	default:
		return false
	}
	text := ""
	if depth == 1 {
		var rawRoot map[string]json.RawMessage
		if json.Unmarshal(c.raw, &rawRoot) == nil {
			var buf bytes.Buffer
			if json.Compact(&buf, rawRoot[key]) == nil {
				text = buf.String()
			}
		}
	}
	if text == "" {
		b, err := json.Marshal(inst)
		if err != nil {
			return false
		}
		text = string(b)
	}
	set(text)
	c.changed = true
	*c.soft = append(*c.soft, "type")
	return true
}

func (c *refCoercer) coercePrimitive(typ, format string, v any) (any, string, bool) {
	switch typ {
	case "string":
		switch x := v.(type) {
		case json.Number:
			// A number where text is expected: its JSON text is the lossless form.
			return x.String(), "type", true
		case string:
			if format == "date-time" {
				if s, ok := normalizeDateTime(x, c.opts.TimeLocation); ok {
					return s, "format", true
				}
			}
		}
	case "integer":
		if s, ok := v.(string); ok && isJSONNumber(s) && !strings.ContainsAny(s, ".eE") {
			if _, err := strconv.ParseInt(s, 10, 64); err == nil {
				return json.Number(s), "type", true
			}
		}
	case "number":
		if s, ok := v.(string); ok && isJSONNumber(s) {
			if f, err := strconv.ParseFloat(s, 64); err == nil && !isInf(f) {
				return json.Number(s), "type", true
			}
		}
	case "boolean":
		if s, ok := v.(string); ok {
			switch strings.ToLower(s) {
			case "true":
				return true, "type", true
			case "false":
				return false, "type", true
			}
		}
	}
	return nil, "", false
}

func refValidateLenient(v *Validator, raw []byte, opts LenientOptions) (out []byte, soft []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var inst any
	if err := dec.Decode(&inst); err != nil {
		return nil, nil, fmt.Errorf("decode instance: %w", err)
	}

	// A JSON null payload stands for "no fields": several stacks send
	// [2,"id","Heartbeat",null]. Treat it as an empty object (soft keyword "type")
	// so a message with no required fields is accepted and one with required
	// fields is still rejected by the validator.
	coerced := false
	if inst == nil {
		inst = map[string]any{}
		raw = []byte("{}")
		coerced = true
		soft = append(soft, "type")
	}

	// Schema-guided coercion runs first so that the validation below (and the
	// typed decode after it) sees the repaired values.
	if refCoerceInstance(v.doc, inst, raw, opts, &soft) {
		coerced = true
	}

	verr := v.schema.Validate(inst)
	if verr == nil {
		if !coerced {
			return raw, soft, nil
		}
		out, mErr := json.Marshal(inst)
		if mErr != nil {
			return nil, nil, fmt.Errorf("re-marshal lenient payload: %w", mErr)
		}
		return out, soft, nil
	}
	var ve *jsonschema.ValidationError
	if !errors.As(verr, &ve) {
		return nil, nil, fmt.Errorf("schema %s: %w", v.name, verr)
	}

	var leaves []*jsonschema.ValidationError
	collectLeaves(ve, &leaves)

	type rewrite struct {
		path []string
		val  string
	}
	var rewrites []rewrite
	for _, leaf := range leaves {
		kw := keywordOf(leaf)
		if kw == "enum" {
			ek, _ := leaf.ErrorKind.(*kind.Enum)
			canon, ok := caseInsensitiveEnumMatch(inst, leaf.InstanceLocation, ek)
			if !ok {
				return nil, nil, hardViolation(v.name, verr, leaf)
			}
			rewrites = append(rewrites, rewrite{path: leaf.InstanceLocation, val: canon})
			soft = append(soft, "enum")
			continue
		}
		if softKeywords[kw] {
			soft = append(soft, kw)
			continue
		}
		return nil, nil, hardViolation(v.name, verr, leaf)
	}

	if len(rewrites) == 0 && !coerced {
		return raw, soft, nil
	}
	for _, rw := range rewrites {
		setStringAtPath(inst, rw.path, rw.val)
	}
	out, mErr := json.Marshal(inst)
	if mErr != nil {
		return nil, nil, fmt.Errorf("re-marshal lenient payload: %w", mErr)
	}
	return out, soft, nil
}
