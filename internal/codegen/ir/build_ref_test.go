package ir

import "testing"

func TestBuildStructTree_NestedAndArray(t *testing.T) {
	schema := map[string]any{
		"title": "MeterValuesRequest",
		"type":  "object",
		"properties": map[string]any{
			"connectorId":   map[string]any{"type": "integer"},
			"transactionId": map[string]any{"type": "integer", "format": "int64"},
			"meterValue": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"timestamp": map[string]any{"type": "string", "format": "date-time"},
						"sampledValue": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"value": map[string]any{"type": "string"},
								},
								"required": []any{"value"},
							},
						},
					},
					"required": []any{"timestamp", "sampledValue"},
				},
			},
		},
		"required": []any{"connectorId", "meterValue"},
	}

	structs, _, err := BuildStructTree("MeterValuesRequest", schema)
	if err != nil {
		t.Fatalf("BuildStructTree() error = %v", err)
	}

	byName := map[string]Struct{}
	for _, s := range structs {
		byName[s.GoName] = s
	}
	for _, name := range []string{"MeterValuesRequest", "MeterValue", "SampledValue"} {
		if _, ok := byName[name]; !ok {
			t.Fatalf("missing struct %s in %#v", name, structs)
		}
	}

	fields := map[string]Field{}
	for _, f := range byName["MeterValuesRequest"].Fields {
		fields[f.JSONName] = f
	}
	if fields["meterValue"].Type != TypeSlice {
		t.Fatalf("meterValue Type = %v, want %v", fields["meterValue"].Type, TypeSlice)
	}
	if fields["meterValue"].ElemType != TypeStructRef {
		t.Fatalf("meterValue ElemType = %v, want %v", fields["meterValue"].ElemType, TypeStructRef)
	}
	if fields["meterValue"].StructRef != "MeterValue" {
		t.Fatalf("meterValue StructRef = %q, want MeterValue", fields["meterValue"].StructRef)
	}
	if fields["transactionId"].Type != TypeInt64 {
		t.Fatalf("transactionId Type = %v, want %v", fields["transactionId"].Type, TypeInt64)
	}
}
