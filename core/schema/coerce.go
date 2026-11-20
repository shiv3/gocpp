package schema

import (
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
func coerceInstance(doc, inst any, opts LenientOptions, soft *[]string) bool {
	root, ok := doc.(map[string]any)
	if !ok {
		return false
	}
	c := &coercer{root: root, opts: opts, soft: soft}
	c.walk(root, inst, nil, 0)
	return c.changed
}

type coercer struct {
	root    map[string]any
	opts    LenientOptions
	soft    *[]string
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

// walk visits inst under schema node. set replaces inst in its parent.
func (c *coercer) walk(node map[string]any, inst any, set func(any), depth int) {
	if depth > maxCoerceDepth {
		return
	}
	node = c.resolve(node)
	switch v := inst.(type) {
	case map[string]any:
		props, _ := node["properties"].(map[string]any)
		for k, child := range v {
			cs, ok := props[k].(map[string]any)
			if !ok {
				continue
			}
			c.walk(cs, child, func(nv any) { v[k] = nv }, depth+1)
		}
	case []any:
		items, ok := node["items"].(map[string]any)
		if !ok {
			return
		}
		for i, child := range v {
			c.walk(items, child, func(nv any) { v[i] = nv }, depth+1)
		}
	default:
		if set == nil {
			return
		}
		typ, _ := node["type"].(string)
		format, _ := node["format"].(string)
		if nv, kw, ok := c.coercePrimitive(typ, format, inst); ok {
			set(nv)
			c.changed = true
			*c.soft = append(*c.soft, kw)
		}
	}
}

func (c *coercer) coercePrimitive(typ, format string, v any) (any, string, bool) {
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
