package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// TimePrecision is the number of fractional-second digits written for an
// outbound date-time.
type TimePrecision int

const (
	// TimePrecisionAuto keeps the value as the message encoder produced it
	// (RFC 3339 with as many fractional digits as the time has, none when zero).
	TimePrecisionAuto TimePrecision = iota
	// TimePrecisionSeconds writes no fractional part, truncating the time.
	TimePrecisionSeconds
	// TimePrecisionMillis writes exactly three fractional digits.
	TimePrecisionMillis
	// TimePrecisionMicros writes exactly six fractional digits.
	TimePrecisionMicros
	// TimePrecisionNanos writes exactly nine fractional digits.
	TimePrecisionNanos
)

// TimeEncoding controls how date-time values are written on the wire. The zero
// value changes nothing. Some chargers reject fractional seconds or a non-UTC
// offset; OCPP itself only requires RFC 3339.
type TimeEncoding struct {
	// UTC converts every date-time to UTC and writes it with a "Z" suffix.
	UTC bool
	// Precision sets the fractional-second digits. Fractions are truncated, never
	// rounded, so a time is never moved into the future.
	Precision TimePrecision
}

// IsZero reports whether the encoding leaves values untouched.
func (e TimeEncoding) IsZero() bool { return !e.UTC && e.Precision == TimePrecisionAuto }

// Format re-encodes one RFC 3339 date-time string. ok is false when s does not
// parse (it is then left as it is) or when the result equals s.
func (e TimeEncoding) Format(s string) (out string, ok bool) {
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		return "", false
	}
	if e.UTC {
		t = t.UTC()
	}
	var layout string
	switch e.Precision {
	case TimePrecisionSeconds:
		layout = "2006-01-02T15:04:05Z07:00"
	case TimePrecisionMillis:
		layout = "2006-01-02T15:04:05.000Z07:00"
	case TimePrecisionMicros:
		layout = "2006-01-02T15:04:05.000000Z07:00"
	case TimePrecisionNanos:
		layout = "2006-01-02T15:04:05.000000000Z07:00"
	default:
		layout = time.RFC3339Nano
	}
	out = t.Format(layout) // Format truncates, it does not round
	return out, out != s
}

// EncodeTimes rewrites every date-time in raw (the fields the schema marks
// "format": "date-time") with enc. It returns raw itself when nothing changes,
// so key order and number formatting survive in that case.
func (v *Validator) EncodeTimes(raw []byte, enc TimeEncoding) ([]byte, error) {
	if enc.IsZero() {
		return raw, nil
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var inst any
	if err := dec.Decode(&inst); err != nil {
		return nil, fmt.Errorf("decode instance: %w", err)
	}
	if !encodeInstanceTimes(v.doc, inst, enc) {
		return raw, nil
	}
	out, err := json.Marshal(inst)
	if err != nil {
		return nil, fmt.Errorf("re-marshal payload: %w", err)
	}
	return out, nil
}
