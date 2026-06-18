package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyDERStartStop21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyDERStartStop", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyDERStartStopRequest{
				ControlID:     "control-1",
				Started:       true,
				SupersededIds: []string{"control-0"},
				Timestamp:     fixedTime21(),
			},
			Valid: true,
		},
		{
			Name: "missing controlId",
			Message: map[string]any{
				"started":   true,
				"timestamp": fixedTime21(),
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength controlId",
			Message: messages.NotifyDERStartStopRequest{
				ControlID: strings.Repeat("x", 37),
				Started:   true,
				Timestamp: fixedTime21(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDERStartStop21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyDERStartStop", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyDERStartStopResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.NotifyDERStartStopResponse{
				CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDERStartStop21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifyDERStartStop)
}
