// Package ir is the version-agnostic intermediate representation used by codegen.
package ir

// Kind enumerates the scalar/composite kinds the generator understands.
type Kind int

const (
	TypeString Kind = iota
	TypeInt32
	TypeInt64
	TypeNumber // decimal
	TypeBool
	TypeDateTime
	TypeEnumRef
	TypeStructRef
	TypeSlice
	TypeMap
)

// Field is one property of a message or nested struct.
type Field struct {
	GoName    string // exported Go field name
	JSONName  string // wire name
	Type      Kind
	ElemType  Kind   // for TypeSlice
	EnumName  string // for TypeEnumRef
	StructRef string // for TypeStructRef
	Required  bool
	MaxLength int // 0 = none
}

// GoType renders the Go type expression for this field.
func (f Field) GoType() string {
	base := f.baseType()
	if !f.Required && f.Type != TypeSlice && f.Type != TypeMap {
		return "*" + base
	}
	return base
}

func (f Field) baseType() string {
	switch f.Type {
	case TypeString:
		return "string"
	case TypeInt32:
		return "int32"
	case TypeInt64:
		return "int64"
	case TypeNumber:
		return "decimal.Decimal"
	case TypeBool:
		return "bool"
	case TypeDateTime:
		return "time.Time"
	case TypeEnumRef:
		return f.EnumName
	case TypeStructRef:
		return f.StructRef
	case TypeSlice:
		return "[]" + (Field{Type: f.ElemType, EnumName: f.EnumName, StructRef: f.StructRef, Required: true}).baseType()
	case TypeMap:
		return "map[string]any"
	default:
		return "any"
	}
}

// Struct is a generated message or nested object.
type Struct struct {
	GoName string
	Fields []Field
}

// Enum is a named string enum with allowed values.
type Enum struct {
	GoName string
	Values []string // wire values
}

// Message binds a request/response struct pair to an action and direction.
type Message struct {
	Action    string
	Direction string // "SentByCP" or "SentByCSMS"
	Request   string // request struct GoName
	Response  string // response struct GoName
}

// File is the full IR for one version, ready to render.
type File struct {
	Version  string // "v16"
	Package  string // "messages"
	Structs  []Struct
	Enums    []Enum
	Messages []Message
}
