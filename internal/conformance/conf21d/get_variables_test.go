package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetVariables21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "GetVariables", "request")

	attributeType := "Actual"
	invalidAttributeType := "InvalidAttributeType"
	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						AttributeType: &attributeType,
						Component:     testComponent(),
						Variable:      testVariable(),
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "missing getVariableData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength component.name",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						Component: messages.ComponentType{Name: longString(51)},
						Variable:  testVariable(),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum attributeType",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						AttributeType: &invalidAttributeType,
						Component:     testComponent(),
						Variable:      testVariable(),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetVariables21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "GetVariables", "response")

	attributeType := "Actual"
	attributeValue := "Available"
	longAttributeValue := longString(2501)
	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeType:   &attributeType,
						AttributeValue:  &attributeValue,
						Component:       testComponent(),
						Variable:        testVariable(),
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "missing getVariableResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength attributeValue",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeValue:  &longAttributeValue,
						Component:       testComponent(),
						Variable:        testVariable(),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum attributeStatus",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "InvalidGetVariableStatus",
						Component:       testComponent(),
						Variable:        testVariable(),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetVariables21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.GetVariables)
}
