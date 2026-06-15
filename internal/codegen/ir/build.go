package ir

import (
	"fmt"
	"sort"
	"strings"
)

// BuildStruct converts a JSON Schema object into an IR Struct plus any enums it
// declares inline. Enum names are derived from the field's Go name.
func BuildStruct(goName string, schema map[string]any) (Struct, []Enum, error) {
	props, _ := schema["properties"].(map[string]any)
	requiredSet := map[string]bool{}
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				requiredSet[s] = true
			}
		}
	}

	s := Struct{GoName: goName}
	var enums []Enum

	// Deterministic field order: sort property names.
	names := make([]string, 0, len(props))
	for name := range props {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		prop, _ := props[name].(map[string]any)
		f := Field{
			GoName:   exportName(name),
			JSONName: name,
			Required: requiredSet[name],
		}
		if err := assignType(&f, prop, &enums); err != nil {
			return Struct{}, nil, fmt.Errorf("field %s: %w", name, err)
		}
		s.Fields = append(s.Fields, f)
	}
	return s, enums, nil
}

func assignType(f *Field, prop map[string]any, enums *[]Enum) error {
	if ml, ok := prop["maxLength"].(float64); ok {
		f.MaxLength = int(ml)
	}
	if enumVals, ok := prop["enum"].([]any); ok {
		vals := make([]string, 0, len(enumVals))
		for _, v := range enumVals {
			vals = append(vals, fmt.Sprint(v))
		}
		enumName := f.GoName
		*enums = append(*enums, Enum{GoName: enumName, Values: vals})
		f.Type = TypeEnumRef
		f.EnumName = enumName
		return nil
	}
	typ, _ := prop["type"].(string)
	switch typ {
	case "string":
		if format, _ := prop["format"].(string); format == "date-time" {
			f.Type = TypeDateTime
		} else {
			f.Type = TypeString
		}
	case "integer":
		f.Type = TypeInt32
	case "number":
		f.Type = TypeNumber
	case "boolean":
		f.Type = TypeBool
	case "array":
		f.Type = TypeSlice
		f.ElemType = TypeString // Phase 0 only needs string arrays; richer arrays land in Phase 1.
	case "object":
		f.Type = TypeMap
	default:
		return fmt.Errorf("unsupported type %q", typ)
	}
	return nil
}

// exportName converts a camelCase JSON name to an exported Go identifier.
func exportName(name string) string {
	if name == "" {
		return ""
	}
	out := strings.ToUpper(name[:1]) + name[1:]
	for _, init := range []struct {
		from string
		to   string
	}{
		{"Iccid", "ICCID"},
		{"Imsi", "IMSI"},
		{"Evse", "EVSE"},
		{"Ocpp", "OCPP"},
		{"Url", "URL"},
		{"Id", "ID"},
	} {
		out = strings.ReplaceAll(out, init.from, init.to)
	}
	return out
}
