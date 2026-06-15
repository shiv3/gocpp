package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestMergeJSONAddsNestedConstraint(t *testing.T) {
	base := mustDecodeJSON(t, `{
	  "type": "object",
	  "properties": {
	    "interval": {
	      "type": "integer"
	    }
	  }
	}`)
	override := mustDecodeJSON(t, `{
	  "properties": {
	    "interval": {
	      "minimum": 0
	    }
	  }
	}`)

	merged := mergeJSON(base, override).(map[string]any)
	interval := merged["properties"].(map[string]any)["interval"].(map[string]any)

	if got, want := interval["type"], "integer"; got != want {
		t.Fatalf("interval.type = %v, want %v", got, want)
	}
	if got, want := interval["minimum"], float64(0); got != want {
		t.Fatalf("interval.minimum = %v, want %v", got, want)
	}
}

func TestMergeJSONEmptyOverrideLeavesBaseUnchanged(t *testing.T) {
	base := mustDecodeJSON(t, `{
	  "type": "object",
	  "properties": {
	    "interval": {
	      "type": "integer"
	    }
	  }
	}`)
	before := mustDecodeJSON(t, `{
	  "type": "object",
	  "properties": {
	    "interval": {
	      "type": "integer"
	    }
	  }
	}`)

	merged := mergeJSON(base, map[string]any{})
	if !reflect.DeepEqual(merged, before) {
		t.Fatalf("mergeJSON(base, empty override) = %#v, want %#v", merged, before)
	}
	if !reflect.DeepEqual(base, before) {
		t.Fatalf("mergeJSON mutated base: got %#v, want %#v", base, before)
	}
}

func mustDecodeJSON(t *testing.T, s string) any {
	t.Helper()
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	return v
}
