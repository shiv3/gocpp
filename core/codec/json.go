// Package codec is the internal JSON seam. It currently delegates to encoding/json;
// a faster library (sonic/go-json) can be swapped here behind a build tag if
// profiling justifies it (spec OQ-04). Hot-path call sites use this package rather
// than encoding/json directly so the implementation can change in one place.
package codec

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"
)

// Marshal serializes v to JSON.
func Marshal(v any) ([]byte, error) { return json.Marshal(v) }

// Unmarshal deserializes data into v. JSON Schema's "integer" type accepts any
// number with a zero fractional part, so a peer may legitimately send 20000.0
// or 2e4 for an integer field (EVerest's libocpp does) while encoding/json
// refuses to put it into an int. On that one failure the payload is retried
// with such numbers rewritten as plain integers; every other payload decodes
// exactly as encoding/json would.
func Unmarshal(data []byte, v any) error {
	err := json.Unmarshal(data, v)
	var ute *json.UnmarshalTypeError
	if err == nil || !errors.As(err, &ute) || !strings.HasPrefix(ute.Value, "number ") {
		return err
	}
	fixed, changed := integralNumbers(data)
	if !changed {
		return err
	}
	if err2 := json.Unmarshal(fixed, v); err2 != nil {
		return err // report the original, not an artefact of the rewrite
	}
	return nil
}

// integralNumbers rewrites every number token (outside strings) that has a
// fraction or exponent but an integral value within int64 range as an integer.
func integralNumbers(b []byte) ([]byte, bool) {
	out := make([]byte, 0, len(b))
	changed := false
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == '"':
			j := i + 1
			for j < len(b) && b[j] != '"' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(b) {
				j = len(b) - 1
			}
			out = append(out, b[i:j+1]...)
			i = j + 1
		case c == '-' || (c >= '0' && c <= '9'):
			j := i + 1
			for j < len(b) && (b[j] >= '0' && b[j] <= '9' || b[j] == '.' || b[j] == 'e' || b[j] == 'E' || b[j] == '+' || b[j] == '-') {
				j++
			}
			tok := string(b[i:j])
			if strings.ContainsAny(tok, ".eE") {
				if f, err := strconv.ParseFloat(tok, 64); err == nil && f == math.Trunc(f) && math.Abs(f) < 1<<53 {
					out = append(out, strconv.FormatInt(int64(f), 10)...)
					changed = true
					i = j
					continue
				}
			}
			out = append(out, tok...)
			i = j
		default:
			out = append(out, c)
			i++
		}
	}
	return out, changed
}
