package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestClearDERControl21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearDERControl", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearDERControlRequest{
				IsDefault: true,
			},
			Valid: true,
		},
		{
			Name:    "missing isDefault",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength controlId",
			Message: messages.ClearDERControlRequest{
				ControlID: stringPtr(longString(37)),
				IsDefault: true,
			},
			Valid: false,
		},
		{
			Name: "invalid enum controlType",
			Message: messages.ClearDERControlRequest{
				ControlType: stringPtr("invalidDERControl"),
				IsDefault:   true,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearDERControl21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ClearDERControl", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearDERControlResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength statusInfo reasonCode",
			Message: messages.ClearDERControlResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.ClearDERControlResponse{
				Status: "invalidDERControlStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearDERControl21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.ClearDERControl)
}
