package schema

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

// softKeywords are JSON-Schema violation keywords that lenient mode logs and
// passes instead of rejecting. Anything not listed here (and any unrecognized
// future keyword) is treated as hard and rejected: fail safe.
var softKeywords = map[string]bool{
	"additionalProperties": true,
	"enum":                 true,
	"minimum":              true,
	"maximum":              true,
	"exclusiveMinimum":     true,
	"exclusiveMaximum":     true,
	"multipleOf":           true,
	"minLength":            true,
	"maxLength":            true,
	"minItems":             true,
	"maxItems":             true,
	"uniqueItems":          true,
	"minProperties":        true,
	"maxProperties":        true,
}

// ValidateLenient validates raw and applies the lenient policy:
//   - err != nil means a hard-class violation is present; the caller rejects.
//   - err == nil means out is the payload (enum case normalized to canonical
//     values, non-RFC3339 date-times and wrong-typed primitives repaired), and
//     soft lists the soft violation keywords seen ("format" and "type" for
//     coercions). out == raw when nothing was rewritten, preserving byte order
//     and numeric formatting.
func (v *Validator) ValidateLenient(raw []byte) (out []byte, soft []string, err error) {
	return v.ValidateLenientWith(raw, LenientOptions{})
}

// ValidateLenientWith is ValidateLenient with explicit options.
func (v *Validator) ValidateLenientWith(raw []byte, opts LenientOptions) (out []byte, soft []string, err error) {
	// {} (modulo whitespace) is the whole payload of Heartbeat.req and of most
	// 1.6 confirmations. When the schema accepts it as is (checked once at
	// construction) there is nothing to decode, coerce or validate. The rest lives
	// in a separate function so this fast path does not pay for the heap-allocated
	// locals of the general one.
	if v.emptyOK && isEmptyObject(raw) {
		return raw, nil, nil
	}
	return v.validateLenient(raw, opts)
}

func (v *Validator) validateLenient(raw []byte, opts LenientOptions) (out []byte, soft []string, err error) {
	inst, ok := fastDecode(raw, v.in)
	if !ok {
		var derr error
		if inst, derr = decodeGeneric(raw); derr != nil {
			return nil, nil, fmt.Errorf("decode instance: %w", derr)
		}
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
	if v.coercible {
		var changed bool
		if soft, changed = coerceInstance(v.doc, inst, raw, opts, soft); changed {
			coerced = true
		}
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

// hardViolation builds the rejection error for the first hard leaf.
func hardViolation(name string, verr error, leaf *jsonschema.ValidationError) error {
	return &ViolationError{
		Keyword: keywordOf(leaf),
		AtRoot:  len(leaf.InstanceLocation) == 0,
		err:     fmt.Errorf("schema %s: %w", name, verr),
	}
}

func collectLeaves(ve *jsonschema.ValidationError, acc *[]*jsonschema.ValidationError) {
	if len(ve.Causes) == 0 {
		*acc = append(*acc, ve)
		return
	}
	for _, c := range ve.Causes {
		collectLeaves(c, acc)
	}
}

func keywordOf(ve *jsonschema.ValidationError) string {
	if ve.ErrorKind == nil {
		return ""
	}
	kp := ve.ErrorKind.KeywordPath()
	if len(kp) == 0 {
		return ""
	}
	return kp[0]
}

func caseInsensitiveEnumMatch(inst any, loc []string, ek *kind.Enum) (string, bool) {
	if ek == nil {
		return "", false
	}
	got, ok := valueAtPath(inst, loc).(string)
	if !ok {
		return "", false
	}
	for _, w := range ek.Want {
		ws, ok := w.(string)
		if ok && strings.EqualFold(ws, got) {
			return ws, true
		}
	}
	return "", false
}

func valueAtPath(inst any, loc []string) any {
	cur := inst
	for _, seg := range loc {
		switch node := cur.(type) {
		case map[string]any:
			cur = node[seg]
		case []any:
			i, err := strconv.Atoi(seg)
			if err != nil || i < 0 || i >= len(node) {
				return nil
			}
			cur = node[i]
		default:
			return nil
		}
	}
	return cur
}

func setStringAtPath(inst any, loc []string, val string) {
	if len(loc) == 0 {
		return
	}
	parent := valueAtPath(inst, loc[:len(loc)-1])
	last := loc[len(loc)-1]
	switch node := parent.(type) {
	case map[string]any:
		node[last] = val
	case []any:
		i, err := strconv.Atoi(last)
		if err == nil && i >= 0 && i < len(node) {
			node[i] = val
		}
	}
}

// decodeGeneric is the reference decoder the fast one falls back to: first JSON
// value of raw, numbers kept as json.Number.
func decodeGeneric(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var inst any
	err := dec.Decode(&inst)
	return inst, err
}

// isEmptyObject reports whether raw is "{}" with optional JSON whitespace around
// and inside the braces.
func isEmptyObject(raw []byte) bool {
	i := 0
	skip := func() {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\r' || raw[i] == '\n') {
			i++
		}
	}
	skip()
	if i >= len(raw) || raw[i] != '{' {
		return false
	}
	i++
	skip()
	if i >= len(raw) || raw[i] != '}' {
		return false
	}
	i++
	skip()
	return i == len(raw)
}
