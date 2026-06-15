// Package conf21b contains OCPP 2.1 GROUP 21-B per-message conformance tests.
package conf21b

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21schemas "github.com/shiv3/gocpp/v21/schemas"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func must21Validator(t *testing.T, action, kind string) *schema.Validator {
	t.Helper()

	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	return conformance.MustValidator(t, reg, v21.Version, action, kind)
}

func requireCPRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v21.SubProtocol))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func requireCSMSRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(v21.SubProtocol))
	defer srv.Close()
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func ptr21[T any](v T) *T {
	return &v
}

func fixedTime21() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func dec21(v string) decimal.Decimal {
	d, err := decimal.NewFromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

func periodicEventStreamParams21() messages.PeriodicEventStreamParamsType {
	return messages.PeriodicEventStreamParamsType{
		Interval: ptr21(int32(60)),
		Values:   ptr21(int32(1)),
	}
}

func constantStreamData21() messages.ConstantStreamDataType {
	return messages.ConstantStreamDataType{
		ID:                   1,
		Params:               periodicEventStreamParams21(),
		VariableMonitoringID: 1,
	}
}

func idToken21() messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: "id-token-1",
		Type:    "Central",
	}
}

func component21() messages.ComponentType {
	return messages.ComponentType{
		Name: "component-1",
	}
}

func variable21() messages.VariableType {
	return messages.VariableType{
		Name: "variable-1",
	}
}

func tariff21() messages.TariffType {
	return messages.TariffType{
		Currency: "EUR",
		TariffID: "tariff-1",
	}
}

func chargingSchedulePeriod21() messages.ChargingSchedulePeriodType {
	return messages.ChargingSchedulePeriodType{
		StartPeriod: 0,
	}
}

func chargingSchedule21() messages.ChargingScheduleType {
	return messages.ChargingScheduleType{
		ID:                     1,
		ChargingRateUnit:       "W",
		ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{chargingSchedulePeriod21()},
	}
}

func chargingProfile21() messages.ChargingProfileType {
	return messages.ChargingProfileType{
		ID:                     1,
		StackLevel:             0,
		ChargingProfilePurpose: "TxProfile",
		ChargingProfileKind:    "Absolute",
		ChargingSchedule:       []messages.ChargingScheduleType{chargingSchedule21()},
	}
}

func setMonitoringData21() messages.SetMonitoringDataType {
	return messages.SetMonitoringDataType{
		Component: component21(),
		Severity:  1,
		Type:      "UpperThreshold",
		Value:     dec21("10.0"),
		Variable:  variable21(),
	}
}

func setMonitoringResult21() messages.SetMonitoringResultType {
	return messages.SetMonitoringResultType{
		Component: component21(),
		Severity:  1,
		Status:    "Accepted",
		Type:      "UpperThreshold",
		Variable:  variable21(),
	}
}

type schemaPathStep21 struct {
	key   string
	array bool
}

type schemaPath21 struct {
	name  string
	steps []schemaPathStep21
}

type schemaMaxPath21 struct {
	schemaPath21
	max int
}

type schemaPaths21 struct {
	required []schemaPath21
	max      []schemaMaxPath21
	enum     []schemaPath21
}

func schemaGeneratedCases21(t *testing.T, action, kind string) []conformance.ValidationCase {
	t.Helper()

	doc := loadSchemaDoc21(t, action, kind)
	full, ok := validSchemaValue21(t, doc, doc, nil).(map[string]any)
	require.True(t, ok, "%s/%s schema root must generate an object", action, kind)

	paths := collectSchemaPaths21(t, doc)
	cases := []conformance.ValidationCase{
		{
			Name:    "valid full schema payload",
			Message: cloneSchemaPayload21(t, full),
			Valid:   true,
		},
	}

	for _, p := range paths.required {
		payload := cloneSchemaPayload21(t, full)
		removeSchemaPath21(t, payload, p.steps)
		cases = append(cases, conformance.ValidationCase{
			Name:    "missing " + p.name,
			Message: payload,
			Valid:   false,
		})
	}
	for _, p := range paths.max {
		payload := cloneSchemaPayload21(t, full)
		setSchemaPath21(t, payload, p.steps, strings.Repeat("x", p.max+1))
		cases = append(cases, conformance.ValidationCase{
			Name:    fmt.Sprintf("%s exceeds maxLength %d", p.name, p.max),
			Message: payload,
			Valid:   false,
		})
	}
	for _, p := range paths.enum {
		payload := cloneSchemaPayload21(t, full)
		setSchemaPath21(t, payload, p.steps, "invalidEnum")
		cases = append(cases, conformance.ValidationCase{
			Name:    "invalid " + p.name + " enum",
			Message: payload,
			Valid:   false,
		})
	}

	return cases
}

func loadSchemaDoc21(t *testing.T, action, kind string) map[string]any {
	t.Helper()

	suffix := "Request"
	if kind == "response" {
		suffix = "Response"
	}
	data, err := fs.ReadFile(v21schemas.FS, action+suffix+".json")
	require.NoError(t, err)

	var doc map[string]any
	require.NoError(t, json.Unmarshal(data, &doc))
	return doc
}

func collectSchemaPaths21(t *testing.T, root map[string]any) schemaPaths21 {
	t.Helper()

	var out schemaPaths21
	collectSchemaPathsInto21(t, root, root, nil, nil, &out)
	return out
}

func collectSchemaPathsInto21(t *testing.T, root, node map[string]any, path []schemaPathStep21, stack []string, out *schemaPaths21) {
	t.Helper()

	node = resolveSchemaRef21(t, root, node, stack)
	if enumValues, ok := node["enum"].([]any); ok && len(enumValues) > 0 {
		out.enum = append(out.enum, schemaPath21{name: formatSchemaPath21(path), steps: cloneSchemaPath21(path)})
	}
	if rawMax, ok := node["maxLength"]; ok {
		out.max = append(out.max, schemaMaxPath21{
			schemaPath21: schemaPath21{name: formatSchemaPath21(path), steps: cloneSchemaPath21(path)},
			max:          intSchemaNumber21(t, rawMax),
		})
	}

	if nodeType21(node) == "array" {
		items, ok := node["items"].(map[string]any)
		require.True(t, ok, "array schema at %s must have object items", formatSchemaPath21(path))
		next := cloneSchemaPath21(path)
		require.NotEmpty(t, next, "array item path must have a parent")
		next[len(next)-1].array = true
		collectSchemaPathsInto21(t, root, items, next, stack, out)
		return
	}

	if required, ok := node["required"].([]any); ok {
		for _, item := range required {
			key, ok := item.(string)
			require.True(t, ok, "required entries must be strings")
			if key == "customData" {
				continue
			}
			steps := append(cloneSchemaPath21(path), schemaPathStep21{key: key})
			out.required = append(out.required, schemaPath21{name: formatSchemaPath21(steps), steps: steps})
		}
	}

	properties, ok := node["properties"].(map[string]any)
	if !ok {
		return
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		if key != "customData" {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		child, ok := properties[key].(map[string]any)
		require.True(t, ok, "property schema for %s must be an object", key)
		steps := append(cloneSchemaPath21(path), schemaPathStep21{key: key})
		collectSchemaPathsInto21(t, root, child, steps, stack, out)
	}
}

func validSchemaValue21(t *testing.T, root, node map[string]any, stack []string) any {
	t.Helper()

	node = resolveSchemaRef21(t, root, node, stack)
	if enumValues, ok := node["enum"].([]any); ok && len(enumValues) > 0 {
		return enumValues[0]
	}

	switch nodeType21(node) {
	case "object":
		payload := make(map[string]any)
		properties, _ := node["properties"].(map[string]any)
		keys := make([]string, 0, len(properties))
		for key := range properties {
			if key != "customData" {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		for _, key := range keys {
			child, ok := properties[key].(map[string]any)
			require.True(t, ok, "property schema for %s must be an object", key)
			payload[key] = validSchemaValue21(t, root, child, stack)
		}
		return payload
	case "array":
		items, ok := node["items"].(map[string]any)
		require.True(t, ok, "array schema must have object items")
		return []any{validSchemaValue21(t, root, items, stack)}
	case "integer":
		return int(validSchemaNumber21(t, node))
	case "number":
		return validSchemaNumber21(t, node)
	case "boolean":
		return true
	case "string":
		if format, _ := node["format"].(string); format == "date-time" {
			return fixedTime21().Format(time.RFC3339)
		}
		if rawMax, ok := node["maxLength"]; ok && intSchemaNumber21(t, rawMax) < len("value") {
			return strings.Repeat("x", intSchemaNumber21(t, rawMax))
		}
		return "value"
	default:
		return nil
	}
}

func validSchemaNumber21(t *testing.T, node map[string]any) float64 {
	t.Helper()

	value := 1.0
	if rawMax, ok := node["maximum"]; ok {
		maxVal := floatSchemaNumber21(t, rawMax)
		if value > maxVal {
			value = maxVal
		}
	}
	if rawMin, ok := node["minimum"]; ok {
		minVal := floatSchemaNumber21(t, rawMin)
		if value < minVal {
			value = minVal
		}
	}
	return value
}

func resolveSchemaRef21(t *testing.T, root, node map[string]any, stack []string) map[string]any {
	t.Helper()

	ref, ok := node["$ref"].(string)
	if !ok {
		return node
	}
	const prefix = "#/definitions/"
	require.True(t, strings.HasPrefix(ref, prefix), "unsupported schema ref %q", ref)
	name := strings.TrimPrefix(ref, prefix)
	if name == "CustomDataType" {
		return map[string]any{"type": "object", "properties": map[string]any{}}
	}
	for _, item := range stack {
		require.NotEqual(t, item, name, "recursive schema ref %q", name)
	}

	defs, ok := root["definitions"].(map[string]any)
	require.True(t, ok, "schema must have definitions for %s", ref)
	raw, ok := defs[name]
	require.True(t, ok, "schema definition %s must exist", name)
	def, ok := raw.(map[string]any)
	require.True(t, ok, "schema definition %s must be an object", name)
	return resolveSchemaRef21(t, root, def, append(stack, name))
}

func nodeType21(node map[string]any) string {
	if typ, ok := node["type"].(string); ok {
		return typ
	}
	if _, ok := node["properties"]; ok {
		return "object"
	}
	if _, ok := node["items"]; ok {
		return "array"
	}
	return ""
}

func intSchemaNumber21(t *testing.T, v any) int {
	t.Helper()

	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		require.Failf(t, "invalid schema number", "unexpected %T", v)
		return 0
	}
}

func floatSchemaNumber21(t *testing.T, v any) float64 {
	t.Helper()

	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	default:
		require.Failf(t, "invalid schema number", "unexpected %T", v)
		return 0
	}
}

func cloneSchemaPath21(path []schemaPathStep21) []schemaPathStep21 {
	if len(path) == 0 {
		return nil
	}
	out := make([]schemaPathStep21, len(path))
	copy(out, path)
	return out
}

func formatSchemaPath21(path []schemaPathStep21) string {
	parts := make([]string, 0, len(path))
	for _, step := range path {
		name := step.key
		if step.array {
			name += "[]"
		}
		parts = append(parts, name)
	}
	return strings.Join(parts, ".")
}

func cloneSchemaPayload21(t *testing.T, src map[string]any) map[string]any {
	t.Helper()

	raw, err := json.Marshal(src)
	require.NoError(t, err)
	var dst map[string]any
	require.NoError(t, json.Unmarshal(raw, &dst))
	return dst
}

func removeSchemaPath21(t *testing.T, payload map[string]any, path []schemaPathStep21) {
	t.Helper()

	require.NotEmpty(t, path, "schema path must not be empty")
	var cur any = payload
	for i, step := range path {
		m, ok := cur.(map[string]any)
		require.True(t, ok, "path %s segment %s must be an object", formatSchemaPath21(path), step.key)
		if step.array {
			raw, ok := m[step.key]
			require.True(t, ok, "array path %s must exist", step.key)
			arr, ok := raw.([]any)
			require.True(t, ok, "path %s segment %s must be an array", formatSchemaPath21(path), step.key)
			require.NotEmpty(t, arr, "array path %s must not be empty", step.key)
			cur = arr[0]
			continue
		}
		if i == len(path)-1 {
			delete(m, step.key)
			return
		}
		raw, ok := m[step.key]
		require.True(t, ok, "object path %s must exist", step.key)
		cur = raw
	}
}

func setSchemaPath21(t *testing.T, payload map[string]any, path []schemaPathStep21, value any) {
	t.Helper()

	require.NotEmpty(t, path, "schema path must not be empty")
	var cur any = payload
	for i, step := range path {
		m, ok := cur.(map[string]any)
		require.True(t, ok, "path %s segment %s must be an object", formatSchemaPath21(path), step.key)
		if step.array {
			raw, ok := m[step.key]
			require.True(t, ok, "array path %s must exist", step.key)
			arr, ok := raw.([]any)
			require.True(t, ok, "path %s segment %s must be an array", formatSchemaPath21(path), step.key)
			require.NotEmpty(t, arr, "array path %s must not be empty", step.key)
			if i == len(path)-1 {
				arr[0] = value
				return
			}
			cur = arr[0]
			continue
		}
		if i == len(path)-1 {
			m[step.key] = value
			return
		}
		raw, ok := m[step.key]
		require.True(t, ok, "object path %s must exist", step.key)
		cur = raw
	}
}
