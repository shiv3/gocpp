package schema

import (
	"encoding/json"
	"testing"
	"testing/fstest"
	"time"

	"github.com/stretchr/testify/require"
)

const coerceTestSchema = `{
  "type": "object",
  "additionalProperties": false,
  "definitions": {
    "Sample": {
      "type": "object",
      "properties": { "at": { "type": "string", "format": "date-time" } }
    }
  },
  "properties": {
    "ts":    { "type": "string", "format": "date-time" },
    "id":    { "type": "integer" },
    "level": { "type": "number" },
    "ok":    { "type": "boolean" },
    "name":  { "type": "string" },
    "list":  { "type": "array", "items": { "$ref": "#/definitions/Sample" } }
  }
}`

func coerceValidator(t *testing.T) *Validator {
	t.Helper()
	fsys := fstest.MapFS{"s.json": &fstest.MapFile{Data: []byte(coerceTestSchema)}}
	v, err := New(fsys, "s.json")
	require.NoError(t, err)
	return v
}

func TestValidateLenient_Coercion(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	cases := []struct {
		name     string
		in       string
		loc      *time.Location
		want     string // JSON expected after rewrite
		wantSoft []string
	}{
		{"zoneless UTC default", `{"ts":"2023-10-19T15:28:07"}`, nil, `{"ts":"2023-10-19T15:28:07Z"}`, []string{"format"}},
		{"zoneless custom loc", `{"ts":"2023-10-19T15:28:07"}`, jst, `{"ts":"2023-10-19T15:28:07+09:00"}`, []string{"format"}},
		{"space separator", `{"ts":"2026-01-01 00:00:00"}`, nil, `{"ts":"2026-01-01T00:00:00Z"}`, []string{"format"}},
		{"lowercase t and z", `{"ts":"2026-01-01t00:00:00z"}`, nil, `{"ts":"2026-01-01T00:00:00Z"}`, []string{"format"}},
		{"comma fraction", `{"ts":"2026-01-01 00:00:00,5"}`, nil, `{"ts":"2026-01-01T00:00:00.5Z"}`, []string{"format"}},
		{"long fraction no zone", `{"ts":"2026-01-01T00:00:00.123456789012"}`, nil, `{"ts":"2026-01-01T00:00:00.123456789Z"}`, []string{"format"}},
		{"offset without colon", `{"ts":"2026-01-01T00:00:00+0900"}`, nil, `{"ts":"2026-01-01T00:00:00+09:00"}`, []string{"format"}},
		{"hour-only offset", `{"ts":"2026-01-01T00:00:00-05"}`, nil, `{"ts":"2026-01-01T00:00:00-05:00"}`, []string{"format"}},
		{"nested via $ref", `{"list":[{"at":"2026-01-01 00:00:00"}]}`, nil, `{"list":[{"at":"2026-01-01T00:00:00Z"}]}`, []string{"format"}},
		{"quoted integer", `{"id":"1"}`, nil, `{"id":1}`, []string{"type"}},
		{"quoted negative integer", `{"id":"-12"}`, nil, `{"id":-12}`, []string{"type"}},
		{"quoted number", `{"level":"32.5"}`, nil, `{"level":32.5}`, []string{"type"}},
		{"quoted number as integer", `{"level":"7"}`, nil, `{"level":7}`, []string{"type"}},
		{"true string", `{"ok":"true"}`, nil, `{"ok":true}`, []string{"type"}},
		{"false string", `{"ok":"false"}`, nil, `{"ok":false}`, []string{"type"}},
		{"number as string", `{"name":123}`, nil, `{"name":"123"}`, []string{"type"}},
		{"float as string keeps text", `{"name":1.50}`, nil, `{"name":"1.50"}`, []string{"type"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, soft, err := coerceValidator(t).ValidateLenientWith([]byte(tc.in), LenientOptions{TimeLocation: tc.loc})
			require.NoError(t, err)
			require.JSONEq(t, tc.want, string(out))
			require.Equal(t, tc.wantSoft, soft)
		})
	}
}

func TestValidateLenient_CoercionLeftAlone(t *testing.T) {
	// Lossy or ambiguous values must stay as sent and be rejected by validation.
	for _, in := range []string{
		`{"id":"1.5"}`, `{"id":"1e2"}`, `{"id":"abc"}`, `{"id":" 1"}`, `{"id":"01"}`, `{"id":""}`,
		`{"level":"NaN"}`, `{"level":"+1"}`, `{"level":"1e999"}`,
		`{"ok":"yes"}`, `{"ok":1}`,
		`{"name":true}`,
	} {
		_, _, err := coerceValidator(t).ValidateLenient([]byte(in))
		require.Error(t, err, in)
	}
}

func TestValidateLenient_UnrepairableDateTimeUnchanged(t *testing.T) {
	// The schema validator does not assert "format", so these pass validation;
	// the point is that they are not rewritten into something else.
	for _, in := range []string{`{"ts":"yesterday"}`, `{"ts":"2026-13-01 00:00:00"}`, `{"ts":"2026-01-01T00:00:00.123456789012Z"}`} {
		out, soft, err := coerceValidator(t).ValidateLenient([]byte(in))
		require.NoError(t, err)
		require.Empty(t, soft, in)
		require.Equal(t, in, string(out))
	}
}

func TestValidateLenient_ValidPayloadUntouched(t *testing.T) {
	raw := []byte(`{"ts":"2026-01-01T00:00:00Z",  "id":1}`)
	out, soft, err := coerceValidator(t).ValidateLenient(raw)
	require.NoError(t, err)
	require.Empty(t, soft)
	require.Equal(t, raw, out)
}

func TestValidateLenient_CoercionCombinesWithEnumAndSoft(t *testing.T) {
	v := lenientValidator(t)
	out, soft, err := v.ValidateLenient([]byte(`{"status":"accepted","count":"3"}`))
	// lenientTestSchema has no coercion on count? it does: integer.
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"type", "enum"}, soft)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "Accepted", m["status"])
	require.EqualValues(t, 3, m["count"])
}
