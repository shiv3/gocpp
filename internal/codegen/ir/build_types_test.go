package ir

import "testing"

func TestBuildStructTree_TypeFidelity(t *testing.T) {
	schema := map[string]any{
		"type":                 "object",
		"additionalProperties": false,
		"definitions": map[string]any{
			"CustomDataType": map[string]any{
				"type":       "object",
				"properties": map[string]any{"vendorId": map[string]any{"type": "string"}},
				"required":   []any{"vendorId"},
			},
		},
		"properties": map[string]any{
			"customData": map[string]any{"$ref": "#/definitions/CustomDataType"},
			"data":       map[string]any{"description": "any JSON"},
			"id":         map[string]any{"type": "integer"},
		},
	}
	structs, _, err := BuildStructTree("Root", schema)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Struct{}
	for _, s := range structs {
		byName[s.GoName] = s
	}
	if byName["Root"].AdditionalProperties {
		t.Error("Root has additionalProperties:false, want closed struct")
	}
	if !byName["CustomDataType"].AdditionalProperties {
		t.Error("CustomDataType leaves additionalProperties open, want Extra support")
	}
	fields := map[string]Field{}
	for _, f := range byName["Root"].Fields {
		fields[f.JSONName] = f
	}
	if got := fields["data"].GoType(); got != "json.RawMessage" {
		t.Errorf("untyped data = %q, want json.RawMessage", got)
	}
	if got := fields["id"].GoType(); got != "*int64" {
		t.Errorf("integer = %q, want *int64", got)
	}
}
