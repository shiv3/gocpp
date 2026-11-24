package schema

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LenientOptions tunes lenient normalization.
type LenientOptions struct {
	// TimeLocation interprets date-times that carry no zone designator. Nil
	// means UTC.
	TimeLocation *time.Location
	// OCPPGo selects the validation policy of upstream ocpp-go v0.19.0: no
	// repairs except the ones its parser accepts too (a zone-less date-time, and
	// structured JSON in a "data" property), no enum case folding, and extra
	// properties are the only tolerated schema violation. Everything else is a
	// hard violation. ocpp-go validates with struct tags, not this schema, so the
	// match is the closest achievable one (see docs/migration-from-ocpp-go.md).
	OCPPGo bool

	// The fields below are opt-in repairs for known non-conforming stacks. They
	// are all off by default and ignored when OCPPGo is set.

	// EmptyNumberAsZero turns an empty string where the schema wants an integer
	// or a number into 0 (soft keyword "type"). Some firmware sends "" for a
	// value it does not have. It is lossy (the value was absent, not zero), hence
	// opt-in.
	EmptyNumberAsZero bool
	// DefaultMissing supplies a value for a required string property the peer
	// left out, keyed by property name (for example {"chargingRateUnit": "W"}).
	// The value is inserted before validation, so it must satisfy the schema; the
	// repair is recorded as soft keyword "required".
	DefaultMissing map[string]string
	// UnknownEnum lists property names (for example "measurand") whose enum
	// violation is passed instead of rejected, so vendor extensions to an
	// otherwise closed value list get through unchanged (soft keyword "enum").
	UnknownEnum []string
}

// coerceInstance walks inst guided by the schema doc and repairs values that a
// non-conforming peer commonly gets wrong but whose intent is unambiguous:
//   - date-time strings in near-RFC3339 forms are rewritten to RFC3339
//     (soft keyword "format");
//   - primitives of the wrong JSON type are converted when the conversion is
//     lossless (soft keyword "type").
//
// It mutates inst in place and reports whether anything changed. Anything it
// cannot repair is left untouched for the validator to reject.
//
// raw is the original payload text; it lets a root-level DataTransfer-style
// "data" property keep its exact bytes when it is turned into a string.
func coerceInstance(doc, inst any, raw []byte, opts LenientOptions, soft []string) ([]string, bool) {
	root, ok := doc.(map[string]any)
	if !ok {
		return soft, false
	}
	c := coercer{root: root, opts: opts, lenient: true, soft: soft, raw: raw}
	c.walk(root, inst, "", nil, 0, 0)
	return c.soft, c.changed
}

// encodeInstanceTimes rewrites every date-time string in inst (found through the
// schema doc) with enc, in place, and reports whether anything changed.
func encodeInstanceTimes(doc, inst any, enc TimeEncoding) bool {
	root, ok := doc.(map[string]any)
	if !ok {
		return false
	}
	c := coercer{root: root, enc: &enc}
	c.walk(root, inst, "", nil, 0, 0)
	return c.changed
}

type coercer struct {
	root map[string]any
	opts LenientOptions
	// lenient selects repair mode; soft collects the soft keywords it records.
	lenient bool
	soft    []string
	raw     []byte
	// enc, when non-nil, switches the walk from lenient repair to outbound
	// date-time re-encoding (encodeInstanceTimes).
	enc     *TimeEncoding
	changed bool
}

const maxCoerceDepth = 64

// resolve follows local "#/definitions/X" references.
func (c *coercer) resolve(node map[string]any) map[string]any {
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
// ("" at the root and in arrays). parent is the map or slice holding inst and idx
// its index when parent is a slice; a nil parent (the root) means inst cannot be
// replaced. Replacing through parent instead of a per-element closure keeps the
// walk allocation-free when nothing needs repair.
func (c *coercer) walk(node map[string]any, inst any, key string, parent any, idx, depth int) {
	if depth > maxCoerceDepth {
		return
	}
	node = c.resolve(node)
	if c.softText(node, inst, key, parent, idx, depth) {
		return
	}
	switch v := inst.(type) {
	case map[string]any:
		props, _ := node["properties"].(map[string]any)
		if c.lenient && len(c.opts.DefaultMissing) > 0 && !c.opts.OCPPGo {
			c.defaultMissing(node, props, v)
		}
		for k, child := range v {
			cs, ok := props[k].(map[string]any)
			if !ok {
				continue
			}
			c.walk(cs, child, k, v, 0, depth+1)
		}
	case []any:
		items, ok := node["items"].(map[string]any)
		if !ok {
			return
		}
		for i, child := range v {
			c.walk(items, child, "", v, i, depth+1)
		}
	default:
		if parent == nil {
			return
		}
		typ, _ := node["type"].(string)
		format, _ := node["format"].(string)
		if nv, kw, ok := c.convert(typ, format, inst); ok {
			replaceIn(parent, key, idx, nv)
			c.changed = true
			if kw != "" && c.lenient {
				c.soft = append(c.soft, kw)
			}
		}
	}
}

// defaultMissing inserts the DefaultMissing value for every required string
// property of node that obj lacks.
func (c *coercer) defaultMissing(node, props map[string]any, obj map[string]any) {
	req, _ := node["required"].([]any)
	for _, r := range req {
		name, _ := r.(string)
		def, ok := c.opts.DefaultMissing[name]
		if !ok {
			continue
		}
		if _, present := obj[name]; present {
			continue
		}
		if ps, _ := props[name].(map[string]any); ps != nil {
			if typ, _ := c.resolve(ps)["type"].(string); typ != "string" {
				continue
			}
		}
		obj[name] = def
		c.changed = true
		c.soft = append(c.soft, "required")
	}
}

// replaceIn stores nv where the walk found its value: parent[key] for a map,
// parent[idx] for a slice.
func replaceIn(parent any, key string, idx int, nv any) {
	switch p := parent.(type) {
	case map[string]any:
		p[key] = nv
	case []any:
		p[idx] = nv
	}
}

// convert applies the active primitive conversion.
func (c *coercer) convert(typ, format string, v any) (any, string, bool) {
	if c.enc == nil {
		return c.coercePrimitive(typ, format, v)
	}
	s, isStr := v.(string)
	if !isStr || format != "date-time" {
		return nil, "", false
	}
	out, ok := c.enc.Format(s)
	return out, "", ok
}

// softText turns a JSON object, array or boolean found under a string-typed
// "data" property into its compact JSON text (OCPP 1.6 DataTransfer.data is a
// string in the schema but many stacks send structured JSON there). The text is
// lossless; at the payload root it is the original bytes, compacted. Only the
// property named "data" is converted: for any other string field a structured
// value is a real error. It reports whether it consumed inst.
func (c *coercer) softText(node map[string]any, inst any, key string, parent any, idx, depth int) bool {
	if !c.lenient || key != "data" || parent == nil {
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
	replaceIn(parent, key, idx, text)
	c.changed = true
	c.soft = append(c.soft, "type")
	return true
}

func (c *coercer) coercePrimitive(typ, format string, v any) (any, string, bool) {
	if c.opts.OCPPGo {
		if s, ok := v.(string); ok && typ == "string" && format == "date-time" && zonelessDateTime.MatchString(s) {
			if out, ok := normalizeDateTime(s, c.opts.TimeLocation); ok {
				return out, "format", true
			}
		}
		return nil, "", false
	}
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
		if s, ok := v.(string); ok && s == "" && c.opts.EmptyNumberAsZero {
			return json.Number("0"), "type", true
		}
		if s, ok := v.(string); ok && isJSONNumber(s) && !strings.ContainsAny(s, ".eE") {
			if _, err := strconv.ParseInt(s, 10, 64); err == nil {
				return json.Number(s), "type", true
			}
		}
	case "number":
		if s, ok := v.(string); ok && s == "" && c.opts.EmptyNumberAsZero {
			return json.Number("0"), "type", true
		}
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

func isInf(f float64) bool { return f > 1.7976931348623157e308 || f < -1.7976931348623157e308 }

// isJSONNumber reports whether s is exactly a JSON number literal (no spaces,
// no leading '+', no leading zeros), so it can be spliced in unchanged.
func isJSONNumber(s string) bool {
	var n json.Number
	if s == "" || json.Unmarshal([]byte(s), &n) != nil {
		return false
	}
	return n.String() == s
}

// zonelessDateTime is the only malformed date-time ocpp-go's parser accepts.
var zonelessDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?$`)

var looseDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[Tt ](\d{2}:\d{2}:\d{2})(?:[.,](\d+))?\s*([Zz]|[+-]\d{2}(?::?\d{2})?)?$`)

// normalizeDateTime rewrites a near-RFC3339 timestamp to RFC3339. It returns
// false when s is already valid RFC3339 or cannot be repaired.
func normalizeDateTime(s string, loc *time.Location) (string, bool) {
	if _, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return "", false
	}
	m := looseDateTime.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	base := m[1] + "T" + m[2]
	if m[3] != "" {
		frac := m[3]
		if len(frac) > 9 {
			frac = frac[:9] // beyond nanoseconds; truncate
		}
		base += "." + frac
	}
	zone := m[4]
	if zone == "" {
		if loc == nil {
			loc = time.UTC
		}
		t, err := time.ParseInLocation("2006-01-02T15:04:05.999999999", base, loc)
		if err != nil {
			return "", false
		}
		return t.Format(time.RFC3339Nano), true
	}
	switch {
	case zone == "z" || zone == "Z":
		zone = "Z"
	case len(zone) == 3: // +HH
		zone += ":00"
	case len(zone) == 5: // +HHMM
		zone = zone[:3] + ":" + zone[3:]
	}
	out := base + zone
	if _, err := time.Parse(time.RFC3339Nano, out); err != nil {
		return "", false
	}
	return out, true
}
