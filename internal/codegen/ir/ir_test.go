package ir

import "testing"

func TestField_GoType(t *testing.T) {
	cases := []struct {
		name  string
		field Field
		want  string
	}{
		{"required string", Field{GoName: "Vendor", Type: TypeString, Required: true}, "string"},
		{"optional string", Field{GoName: "Iccid", Type: TypeString, Required: false}, "*string"},
		{"required int", Field{GoName: "Interval", Type: TypeInt32, Required: true}, "int32"},
		{"required time", Field{GoName: "Now", Type: TypeDateTime, Required: true}, "time.Time"},
		{"enum ref", Field{GoName: "Status", Type: TypeEnumRef, EnumName: "RegistrationStatus", Required: true}, "RegistrationStatus"},
		{"slice", Field{GoName: "Items", Type: TypeSlice, ElemType: TypeString, Required: true}, "[]string"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.field.GoType(); got != tc.want {
				t.Errorf("GoType() = %q, want %q", got, tc.want)
			}
		})
	}
}
