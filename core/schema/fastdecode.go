package schema

import (
	"encoding/json"
	"unicode/utf8"
)

// fastDecode is a small recursive-descent decoder for the lenient path. It
// produces exactly the tree json.Decoder with UseNumber produces
// (map[string]any, []any, string, json.Number, bool, nil) without reflection, and
// like Decoder.Decode it reads one value and ignores whatever follows it.
//
// It handles the common case only: a top-level object whose strings contain no
// escape sequences, no control characters and only valid UTF-8, with nesting up
// to maxFastDepth. For anything else, including every malformed input, it reports
// ok=false and the caller runs the general decoder, so error behaviour is
// unchanged.
//
// intern, when non-nil, supplies pre-built strings for property names and enum
// values that appear in the schema; using them avoids one allocation per key and
// per known value. The tree is owned by the caller and never pooled.
func fastDecode(raw []byte, in *interner) (inst any, ok bool) {
	// One copy of the payload backs every number and string in the tree (as
	// substrings) instead of one small allocation each. The tree lives only as long
	// as the validation call, so keeping the whole copy alive costs nothing.
	d := fastDecoder{b: raw, src: string(raw), in: in}
	d.skipSpace()
	if d.i >= len(d.b) || d.b[d.i] != '{' {
		return nil, false
	}
	m, ok := d.object(0)
	if !ok {
		return nil, false
	}
	return m, true
}

const maxFastDepth = 64

// interner holds strings found in a schema (property names, string enum values).
// It is built once per Validator and only read afterwards.
type interner struct {
	keys map[string]string
	vals map[string]any // the same strings, already boxed in an interface
}

func newInterner() *interner {
	return &interner{keys: map[string]string{}, vals: map[string]any{}}
}

func (in *interner) add(s string) {
	if _, ok := in.keys[s]; !ok {
		in.keys[s] = s
		in.vals[s] = s
	}
}

type fastDecoder struct {
	b   []byte
	src string // string(b)
	i   int
	in  *interner
}

func (d *fastDecoder) skipSpace() {
	for d.i < len(d.b) {
		switch d.b[d.i] {
		case ' ', '\t', '\r', '\n':
			d.i++
		default:
			return
		}
	}
}

// str scans a string token at d.i (a '"') and returns the byte range of its
// content, quotes excluded.
func (d *fastDecoder) str() (start, end int, ok bool) {
	d.i++
	start = d.i
	high := false
	for d.i < len(d.b) {
		c := d.b[d.i]
		switch {
		case c == '"':
			end = d.i
			d.i++
			if high && !utf8.Valid(d.b[start:end]) {
				return 0, 0, false
			}
			return start, end, true
		case c == '\\' || c < 0x20:
			return 0, 0, false
		case c >= 0x80:
			high = true
		}
		d.i++
	}
	return 0, 0, false
}

func (d *fastDecoder) key(start, end int) string {
	if d.in != nil {
		if k, ok := d.in.keys[string(d.b[start:end])]; ok { // no allocation for the lookup
			return k
		}
	}
	return d.src[start:end]
}

func (d *fastDecoder) value(depth int) (any, bool) {
	d.skipSpace()
	if d.i >= len(d.b) {
		return nil, false
	}
	switch c := d.b[d.i]; {
	case c == '{':
		return d.object(depth)
	case c == '[':
		return d.array(depth)
	case c == '"':
		start, end, ok := d.str()
		if !ok {
			return nil, false
		}
		if d.in != nil {
			if v, ok := d.in.vals[string(d.b[start:end])]; ok {
				return v, true
			}
		}
		return d.src[start:end], true
	case c == '-' || (c >= '0' && c <= '9'):
		return d.number()
	case c == 't':
		return d.literal("true", true)
	case c == 'f':
		return d.literal("false", false)
	case c == 'n':
		return d.literal("null", nil)
	}
	return nil, false
}

func (d *fastDecoder) literal(word string, v any) (any, bool) {
	if len(d.b)-d.i < len(word) || string(d.b[d.i:d.i+len(word)]) != word {
		return nil, false
	}
	d.i += len(word)
	if !d.atDelimiter() {
		return nil, false
	}
	return v, true
}

// atDelimiter reports whether the byte at d.i ends a scalar value.
func (d *fastDecoder) atDelimiter() bool {
	if d.i >= len(d.b) {
		return false // a scalar cannot end an object or array
	}
	switch d.b[d.i] {
	case ',', '}', ']', ' ', '\t', '\r', '\n':
		return true
	}
	return false
}

// number accepts the JSON number grammar and returns the literal as json.Number.
func (d *fastDecoder) number() (any, bool) {
	start := d.i
	if d.b[d.i] == '-' {
		d.i++
	}
	if d.i >= len(d.b) {
		return nil, false
	}
	switch c := d.b[d.i]; {
	case c == '0':
		d.i++
	case c >= '1' && c <= '9':
		for d.i < len(d.b) && d.b[d.i] >= '0' && d.b[d.i] <= '9' {
			d.i++
		}
	default:
		return nil, false
	}
	if d.i < len(d.b) && d.b[d.i] == '.' {
		d.i++
		n := 0
		for d.i < len(d.b) && d.b[d.i] >= '0' && d.b[d.i] <= '9' {
			d.i++
			n++
		}
		if n == 0 {
			return nil, false
		}
	}
	if d.i < len(d.b) && (d.b[d.i] == 'e' || d.b[d.i] == 'E') {
		d.i++
		if d.i < len(d.b) && (d.b[d.i] == '+' || d.b[d.i] == '-') {
			d.i++
		}
		n := 0
		for d.i < len(d.b) && d.b[d.i] >= '0' && d.b[d.i] <= '9' {
			d.i++
			n++
		}
		if n == 0 {
			return nil, false
		}
	}
	if !d.atDelimiter() {
		return nil, false
	}
	return json.Number(d.src[start:d.i]), true
}

func (d *fastDecoder) array(depth int) (any, bool) {
	if depth >= maxFastDepth {
		return nil, false
	}
	d.i++ // '['
	arr := []any{}
	d.skipSpace()
	if d.i < len(d.b) && d.b[d.i] == ']' {
		d.i++
		return arr, true
	}
	for {
		v, ok := d.value(depth + 1)
		if !ok {
			return nil, false
		}
		arr = append(arr, v)
		d.skipSpace()
		if d.i >= len(d.b) {
			return nil, false
		}
		switch d.b[d.i] {
		case ',':
			d.i++
		case ']':
			d.i++
			return arr, true
		default:
			return nil, false
		}
	}
}

func (d *fastDecoder) object(depth int) (map[string]any, bool) {
	if depth >= maxFastDepth {
		return nil, false
	}
	d.i++ // '{'
	m := map[string]any{}
	d.skipSpace()
	if d.i < len(d.b) && d.b[d.i] == '}' {
		d.i++
		return m, true
	}
	for {
		d.skipSpace()
		if d.i >= len(d.b) || d.b[d.i] != '"' {
			return nil, false
		}
		ks, ke, ok := d.str()
		if !ok {
			return nil, false
		}
		k := d.key(ks, ke)
		d.skipSpace()
		if d.i >= len(d.b) || d.b[d.i] != ':' {
			return nil, false
		}
		d.i++
		v, ok := d.value(depth + 1)
		if !ok {
			return nil, false
		}
		m[k] = v
		d.skipSpace()
		if d.i >= len(d.b) {
			return nil, false
		}
		switch d.b[d.i] {
		case ',':
			d.i++
		case '}':
			d.i++
			return m, true
		default:
			return nil, false
		}
	}
}
