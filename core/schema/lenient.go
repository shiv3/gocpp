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
//     values when an enum drifted only by case), and soft lists the soft
//     violation keywords seen. out == raw when no enum rewrite was needed,
//     preserving byte order and numeric formatting.
func (v *Validator) ValidateLenient(raw []byte) (out []byte, soft []string, err error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var inst any
	if err := dec.Decode(&inst); err != nil {
		return nil, nil, fmt.Errorf("decode instance: %w", err)
	}

	verr := v.schema.Validate(inst)
	if verr == nil {
		return raw, nil, nil
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
				return nil, nil, fmt.Errorf("schema %s: %w", v.name, verr)
			}
			rewrites = append(rewrites, rewrite{path: leaf.InstanceLocation, val: canon})
			soft = append(soft, "enum")
			continue
		}
		if softKeywords[kw] {
			soft = append(soft, kw)
			continue
		}
		return nil, nil, fmt.Errorf("schema %s: %w", v.name, verr)
	}

	if len(rewrites) == 0 {
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
