package schema

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const dataSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "vendorId": { "type": "string" },
    "data":     { "type": "string" },
    "other":    { "type": "string" },
    "when":     { "type": "string", "format": "date-time" },
    "list": { "type": "array", "items": { "type": "object", "properties": {
      "at": { "type": "string", "format": "date-time" } } } }
  },
  "required": ["vendorId"]
}`

func dataValidator(t *testing.T) *Validator {
	t.Helper()
	v, err := New(fstest.MapFS{"s.json": &fstest.MapFile{Data: []byte(dataSchema)}}, "s.json")
	require.NoError(t, err)
	return v
}

func TestValidateLenient_NullPayload(t *testing.T) {
	// No required fields: null stands for {}.
	v, err := New(fstest.MapFS{"s.json": &fstest.MapFile{Data: []byte(`{"type":"object","additionalProperties":false,"properties":{}}`)}}, "s.json")
	require.NoError(t, err)
	out, soft, err := v.ValidateLenient([]byte(`null`))
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(out))
	require.Equal(t, []string{"type"}, soft)

	// Strict validation still rejects it.
	require.Error(t, v.Validate([]byte(`null`)))

	// Required fields are still enforced after the null is treated as {}.
	_, _, err = dataValidator(t).ValidateLenient([]byte(`null`))
	var ve *ViolationError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, "required", ve.Keyword)
}

func TestValidateLenient_StructuredDataBecomesText(t *testing.T) {
	v := dataValidator(t)
	for name, tc := range map[string]struct{ in, want string }{
		// Root level: the original bytes (key order, number text) survive, compacted.
		"object":  {`{"vendorId":"v","data":{ "b":1.50, "a":[1, 2] }}`, `{"vendorId":"v","data":"{\"b\":1.50,\"a\":[1,2]}"}`},
		"array":   {`{"vendorId":"v","data":[1, "x"]}`, `{"vendorId":"v","data":"[1,\"x\"]"}`},
		"number":  {`{"vendorId":"v","data":12.0}`, `{"vendorId":"v","data":"12.0"}`},
		"bool":    {`{"vendorId":"v","data":true}`, `{"vendorId":"v","data":"true"}`},
		"already": {`{"vendorId":"v","data":"plain"}`, `{"vendorId":"v","data":"plain"}`},
	} {
		t.Run(name, func(t *testing.T) {
			out, _, err := v.ValidateLenient([]byte(tc.in))
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(out))
			var m struct{ Data string }
			require.NoError(t, json.Unmarshal(out, &m))
		})
	}

	_, soft, err := v.ValidateLenient([]byte(`{"vendorId":"v","data":{"a":1}}`))
	require.NoError(t, err)
	require.Equal(t, []string{"type"}, soft)

	// Only "data" gets this treatment: a structured value elsewhere is an error.
	_, _, err = v.ValidateLenient([]byte(`{"vendorId":"v","other":{"a":1}}`))
	require.Error(t, err)
	// Strict stays strict.
	require.Error(t, v.Validate([]byte(`{"vendorId":"v","data":{"a":1}}`)))
}

func TestTimeEncoding_Format(t *testing.T) {
	cases := []struct {
		name string
		enc  TimeEncoding
		in   string
		want string
		ok   bool
	}{
		{"zero value untouched", TimeEncoding{}, "2026-01-01T09:00:00.123+09:00", "", false},
		{"utc only", TimeEncoding{UTC: true}, "2026-01-01T09:00:00.123+09:00", "2026-01-01T00:00:00.123Z", true},
		{"seconds truncates", TimeEncoding{Precision: TimePrecisionSeconds}, "2026-01-01T00:00:00.999999999Z", "2026-01-01T00:00:00Z", true},
		{"seconds keeps offset", TimeEncoding{Precision: TimePrecisionSeconds}, "2026-01-01T09:00:00.5+09:00", "2026-01-01T09:00:00+09:00", true},
		{"utc seconds", TimeEncoding{UTC: true, Precision: TimePrecisionSeconds}, "2026-01-01T09:00:00.5+09:00", "2026-01-01T00:00:00Z", true},
		{"millis fixed width", TimeEncoding{Precision: TimePrecisionMillis}, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00.000Z", true},
		{"millis truncates", TimeEncoding{Precision: TimePrecisionMillis}, "2026-01-01T00:00:00.123456789Z", "2026-01-01T00:00:00.123Z", true},
		{"micros", TimeEncoding{Precision: TimePrecisionMicros}, "2026-01-01T00:00:00.1Z", "2026-01-01T00:00:00.100000Z", true},
		{"nanos", TimeEncoding{Precision: TimePrecisionNanos}, "2026-01-01T00:00:00Z", "2026-01-01T00:00:00.000000000Z", true},
		{"already in form", TimeEncoding{UTC: true, Precision: TimePrecisionSeconds}, "2026-01-01T00:00:00Z", "", false},
		{"not a date", TimeEncoding{UTC: true}, "yesterday", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.enc.Format(tc.in)
			require.Equal(t, tc.ok, ok)
			if ok {
				require.Equal(t, tc.want, got)
			}
		})
	}
}

func TestEncodeTimes_OnlySchemaDateTimeFields(t *testing.T) {
	v := dataValidator(t)
	enc := TimeEncoding{UTC: true, Precision: TimePrecisionSeconds}
	out, err := v.EncodeTimes([]byte(`{"vendorId":"v","when":"2026-01-01T09:00:00.5+09:00","other":"2026-01-01T09:00:00.5+09:00","list":[{"at":"2026-01-01T09:00:00.25+09:00"}]}`), enc)
	require.NoError(t, err)
	require.JSONEq(t, `{"vendorId":"v","when":"2026-01-01T00:00:00Z","other":"2026-01-01T09:00:00.5+09:00","list":[{"at":"2026-01-01T00:00:00Z"}]}`, string(out))

	// Nothing to change: the input bytes come back untouched.
	in := []byte(`{ "when": "2026-01-01T00:00:00Z",  "vendorId": "v" }`)
	out, err = v.EncodeTimes(in, enc)
	require.NoError(t, err)
	require.Equal(t, string(in), string(out))

	// Zero encoding is a no-op.
	out, err = v.EncodeTimes([]byte(`{"when":"2026-01-01T09:00:00.5+09:00"}`), TimeEncoding{})
	require.NoError(t, err)
	require.Contains(t, string(out), "+09:00")
}
