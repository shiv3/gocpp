package render

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/codegen/ir"
)

func TestMessages_EnumValidMethod(t *testing.T) {
	f := ir.File{
		Package: "messages",
		Enums: []ir.Enum{
			{GoName: "RegistrationStatus", Values: []string{"Accepted", "Pending", "Rejected"}},
		},
	}
	out, err := Messages(f)
	if err != nil {
		t.Fatalf("Messages() error = %v", err)
	}
	src := string(out)
	for _, want := range []string{
		"func (e RegistrationStatus) Valid() bool",
		"case RegistrationStatusAccepted, RegistrationStatusPending, RegistrationStatusRejected:",
	} {
		if !strings.Contains(src, want) {
			t.Fatalf("source does not contain %q:\n%s", want, src)
		}
	}
}
