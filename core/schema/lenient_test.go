package schema

import (
	"encoding/json"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

const lenientTestSchema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "status": { "type": "string", "enum": ["Accepted", "Rejected"] },
    "count":  { "type": "integer", "minimum": 0 },
    "name":   { "type": "string", "minLength": 1 }
  },
  "required": ["status"]
}`

func lenientValidator(t *testing.T) *Validator {
	t.Helper()
	fsys := fstest.MapFS{"s.json": &fstest.MapFile{Data: []byte(lenientTestSchema)}}
	v, err := New(fsys, "s.json")
	require.NoError(t, err)
	return v
}

func TestValidateLenient_SoftExtraFieldPassesUnchanged(t *testing.T) {
	v := lenientValidator(t)
	raw := []byte(`{"status":"Accepted","extra":1}`)
	out, soft, err := v.ValidateLenient(raw)
	require.NoError(t, err)
	require.Equal(t, []string{"additionalProperties"}, soft)
	require.Equal(t, raw, out)
}

func TestValidateLenient_SoftBoundsPass(t *testing.T) {
	v := lenientValidator(t)
	out, soft, err := v.ValidateLenient([]byte(`{"status":"Accepted","count":-1,"name":""}`))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"minimum", "minLength"}, soft)
	require.NotNil(t, out)
}

func TestValidateLenient_EnumCaseNormalized(t *testing.T) {
	v := lenientValidator(t)
	out, soft, err := v.ValidateLenient([]byte(`{"status":"accepted"}`))
	require.NoError(t, err)
	require.Equal(t, []string{"enum"}, soft)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "Accepted", m["status"])
}

func TestValidateLenient_UnknownEnumIsHard(t *testing.T) {
	v := lenientValidator(t)
	_, _, err := v.ValidateLenient([]byte(`{"status":"Teleport"}`))
	require.Error(t, err)
}

func TestValidateLenient_TypeMismatchIsHard(t *testing.T) {
	v := lenientValidator(t)
	_, _, err := v.ValidateLenient([]byte(`{"status":123}`))
	require.Error(t, err)
}

func TestValidateLenient_MissingRequiredIsHard(t *testing.T) {
	v := lenientValidator(t)
	_, _, err := v.ValidateLenient([]byte(`{}`))
	require.Error(t, err)
}

func TestValidateLenient_MixedSoftRewritesAndPasses(t *testing.T) {
	v := lenientValidator(t)
	out, soft, err := v.ValidateLenient([]byte(`{"status":"accepted","bad":1,"count":-1}`))
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"enum", "additionalProperties", "minimum"}, soft)
	var m map[string]any
	require.NoError(t, json.Unmarshal(out, &m))
	require.Equal(t, "Accepted", m["status"])
}

func TestValidateLenient_AnyHardRejectsWholeMessage(t *testing.T) {
	v := lenientValidator(t)
	_, _, err := v.ValidateLenient([]byte(`{"status":"accepted","count":"notnum"}`))
	require.Error(t, err)
}

func TestValidateLenient_NumericPrecisionPreservedThroughRewrite(t *testing.T) {
	v := lenientValidator(t)
	out, _, err := v.ValidateLenient([]byte(`{"status":"accepted","count":9007199254740993}`))
	require.NoError(t, err)
	require.Contains(t, string(out), "9007199254740993")
}

func TestValidateLenient_ValidPassesThrough(t *testing.T) {
	v := lenientValidator(t)
	raw := []byte(`{"status":"Accepted","count":3}`)
	out, soft, err := v.ValidateLenient(raw)
	require.NoError(t, err)
	require.Empty(t, soft)
	require.Equal(t, raw, out)
}
