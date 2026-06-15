package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestSetDERControl21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SetDERControl", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: setDERControlRequest21(),
			Valid:   true,
		},
		{
			Name: "missing controlId",
			Message: map[string]any{
				"controlType": "VoltVar",
				"isDefault":   false,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.SetDERControlRequest{
				ControlID:   longString(37),
				ControlType: "VoltVar",
				IsDefault:   false,
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.SetDERControlRequest{
				ControlID:   "control-1",
				ControlType: "BogusControl",
				IsDefault:   false,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDERControl21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SetDERControl", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetDERControlResponse{
				Status:        "Accepted",
				SupersededIds: []string{"old-control-1"},
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.SetDERControlResponse{
				Status:        "Accepted",
				SupersededIds: []string{longString(37)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.SetDERControlResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDERControl21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.SetDERControl)
}
