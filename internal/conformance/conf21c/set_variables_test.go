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

func TestSetVariables21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SetVariables", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					{
						AttributeType:  ptr("Actual"),
						AttributeValue: "value",
						Component:      component21(),
						Variable:       variable21(),
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "missing setVariableData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					{
						AttributeValue: longString(2501),
						Component:      component21(),
						Variable:       variable21(),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					{
						AttributeType:  ptr("BogusAttribute"),
						AttributeValue: "value",
						Component:      component21(),
						Variable:       variable21(),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariables21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SetVariables", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeType:   ptr("Actual"),
						Component:       component21(),
						Variable:        variable21(),
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "missing setVariableResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					{
						AttributeStatus: "Accepted",
						Component:       messages.ComponentType{Name: longString(51)},
						Variable:        variable21(),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					{
						AttributeStatus: "BogusStatus",
						Component:       component21(),
						Variable:        variable21(),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariables21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.SetVariables)
}
