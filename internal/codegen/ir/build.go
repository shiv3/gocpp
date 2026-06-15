package ir

import (
	"fmt"
	"sort"

	"github.com/shiv3/gocpp/internal/codegen/naming"
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
			GoName:   naming.Export(name),
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

// BuildStructTree converts a schema and all nested object types into a flat
// list of Structs, with the root struct first, plus all inline enums.
func BuildStructTree(rootName string, schema map[string]any) ([]Struct, []Enum, error) {
	b := treeBuilder{seen: map[string]bool{}}
	if err := b.build(rootName, schema); err != nil {
		return nil, nil, err
	}
	return b.structs, b.enums, nil
}

type treeBuilder struct {
	structs []Struct
	enums   []Enum
	seen    map[string]bool
}

type childSchema struct {
	goName string
	schema map[string]any
}

func (b *treeBuilder) build(goName string, schema map[string]any) error {
	if b.seen[goName] {
		return nil
	}
	b.seen[goName] = true

	props, _ := schema["properties"].(map[string]any)
	requiredSet := requiredSet(schema)
	s := Struct{GoName: goName}
	children := make([]childSchema, 0)

	for _, name := range sortedKeys(props) {
		prop, _ := props[name].(map[string]any)
		f := Field{
			GoName:   naming.Export(name),
			JSONName: name,
			Required: requiredSet[name],
		}
		child, err := b.assignTreeType(goName, name, &f, prop)
		if err != nil {
			return fmt.Errorf("field %s: %w", name, err)
		}
		if child.goName != "" {
			children = append(children, child)
		}
		s.Fields = append(s.Fields, f)
	}

	b.structs = append(b.structs, s)
	for _, child := range children {
		if err := b.build(child.goName, child.schema); err != nil {
			return err
		}
	}
	return nil
}

func (b *treeBuilder) assignTreeType(parentName, fieldName string, f *Field, prop map[string]any) (childSchema, error) {
	if ml, ok := prop["maxLength"].(float64); ok {
		f.MaxLength = int(ml)
	}
	if enumVals, ok := prop["enum"].([]any); ok {
		vals := make([]string, 0, len(enumVals))
		for _, v := range enumVals {
			vals = append(vals, fmt.Sprint(v))
		}
		enumName := parentName + naming.Export(fieldName)
		b.enums = append(b.enums, Enum{GoName: enumName, Values: vals})
		f.Type = TypeEnumRef
		f.EnumName = enumName
		return childSchema{}, nil
	}

	typ, _ := prop["type"].(string)
	switch typ {
	case "string", "integer", "number", "boolean":
		assignScalar(f, prop)
	case "array":
		f.Type = TypeSlice
		items, _ := prop["items"].(map[string]any)
		if enumVals, ok := items["enum"].([]any); ok {
			vals := make([]string, 0, len(enumVals))
			for _, v := range enumVals {
				vals = append(vals, fmt.Sprint(v))
			}
			enumName := parentName + naming.Export(fieldName)
			b.enums = append(b.enums, Enum{GoName: enumName, Values: vals})
			f.ElemType = TypeEnumRef
			f.EnumName = enumName
			return childSchema{}, nil
		}
		itemType, _ := items["type"].(string)
		if itemType == "object" && items["properties"] != nil {
			childName := singular(naming.Export(fieldName))
			f.ElemType = TypeStructRef
			f.StructRef = childName
			return childSchema{goName: childName, schema: items}, nil
		}
		f.ElemType = scalarKind(itemType, items)
	case "object":
		if prop["properties"] == nil {
			f.Type = TypeMap
			return childSchema{}, nil
		}
		childName := naming.Export(fieldName)
		f.Type = TypeStructRef
		f.StructRef = childName
		return childSchema{goName: childName, schema: prop}, nil
	default:
		f.Type = TypeMap
	}
	return childSchema{}, nil
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
		assignScalar(f, prop)
	case "integer":
		assignScalar(f, prop)
	case "number":
		assignScalar(f, prop)
	case "boolean":
		assignScalar(f, prop)
	case "array":
		f.Type = TypeSlice
		items, _ := prop["items"].(map[string]any)
		itemType, _ := items["type"].(string)
		f.ElemType = scalarKind(itemType, items)
	case "object":
		f.Type = TypeMap
	default:
		return fmt.Errorf("unsupported type %q", typ)
	}
	return nil
}

func assignScalar(f *Field, prop map[string]any) {
	typ, _ := prop["type"].(string)
	switch typ {
	case "string":
		if format, _ := prop["format"].(string); format == "date-time" {
			f.Type = TypeDateTime
			return
		}
		f.Type = TypeString
	case "integer":
		if format, _ := prop["format"].(string); format == "int64" {
			f.Type = TypeInt64
			return
		}
		f.Type = TypeInt32
	case "number":
		f.Type = TypeNumber
	case "boolean":
		f.Type = TypeBool
	default:
		f.Type = TypeMap
	}
}

func scalarKind(typ string, prop map[string]any) Kind {
	f := Field{}
	if prop == nil {
		prop = map[string]any{"type": typ}
	}
	assignScalar(&f, prop)
	return f.Type
}

func requiredSet(schema map[string]any) map[string]bool {
	out := map[string]bool{}
	if req, ok := schema["required"].([]any); ok {
		for _, r := range req {
			if s, ok := r.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func singular(name string) string {
	if len(name) > 1 && name[len(name)-1] == 's' {
		return name[:len(name)-1]
	}
	return name
}
