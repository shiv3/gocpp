package ir

import (
	"reflect"
	"testing"
)

func TestBuildStruct_BootNotificationRequest(t *testing.T) {
	schema := map[string]any{
		"title": "BootNotificationRequest",
		"type":  "object",
		"properties": map[string]any{
			"chargePointVendor": map[string]any{"type": "string", "maxLength": float64(20)},
			"chargePointModel":  map[string]any{"type": "string", "maxLength": float64(20)},
			"iccid":             map[string]any{"type": "string", "maxLength": float64(20)},
		},
		"required": []any{"chargePointVendor", "chargePointModel"},
	}
	s, enums, err := BuildStruct("BootNotificationRequest", schema)
	if err != nil {
		t.Fatalf("BuildStruct() error = %v", err)
	}
	if len(enums) != 0 {
		t.Fatalf("enums len = %d, want 0", len(enums))
	}
	if s.GoName != "BootNotificationRequest" {
		t.Fatalf("GoName = %q, want %q", s.GoName, "BootNotificationRequest")
	}
	if len(s.Fields) != 3 {
		t.Fatalf("fields len = %d, want 3", len(s.Fields))
	}

	byName := map[string]Field{}
	for _, f := range s.Fields {
		byName[f.JSONName] = f
	}
	if !byName["chargePointVendor"].Required {
		t.Fatalf("chargePointVendor Required = false, want true")
	}
	if byName["chargePointVendor"].MaxLength != 20 {
		t.Fatalf("chargePointVendor MaxLength = %d, want 20", byName["chargePointVendor"].MaxLength)
	}
	if byName["iccid"].Required {
		t.Fatalf("iccid Required = true, want false")
	}
	if byName["chargePointVendor"].GoName != "ChargePointVendor" {
		t.Fatalf("chargePointVendor GoName = %q, want %q", byName["chargePointVendor"].GoName, "ChargePointVendor")
	}
}

func TestBuildStruct_EnumExtraction(t *testing.T) {
	schema := map[string]any{
		"title": "BootNotificationResponse",
		"type":  "object",
		"properties": map[string]any{
			"status":      map[string]any{"type": "string", "enum": []any{"Accepted", "Pending", "Rejected"}},
			"currentTime": map[string]any{"type": "string", "format": "date-time"},
			"interval":    map[string]any{"type": "number"},
		},
		"required": []any{"status", "currentTime", "interval"},
	}
	s, enums, err := BuildStruct("BootNotificationResponse", schema)
	if err != nil {
		t.Fatalf("BuildStruct() error = %v", err)
	}
	if len(enums) != 1 {
		t.Fatalf("enums len = %d, want 1", len(enums))
	}
	if enums[0].GoName != "Status" {
		t.Fatalf("enum GoName = %q, want %q", enums[0].GoName, "Status")
	}
	if !reflect.DeepEqual(enums[0].Values, []string{"Accepted", "Pending", "Rejected"}) {
		t.Fatalf("enum values = %#v, want %#v", enums[0].Values, []string{"Accepted", "Pending", "Rejected"})
	}

	byName := map[string]Field{}
	for _, f := range s.Fields {
		byName[f.JSONName] = f
	}
	if byName["status"].Type != TypeEnumRef {
		t.Fatalf("status Type = %v, want %v", byName["status"].Type, TypeEnumRef)
	}
	if byName["currentTime"].Type != TypeDateTime {
		t.Fatalf("currentTime Type = %v, want %v", byName["currentTime"].Type, TypeDateTime)
	}
	if byName["interval"].Type != TypeNumber {
		t.Fatalf("interval Type = %v, want %v", byName["interval"].Type, TypeNumber)
	}
}
