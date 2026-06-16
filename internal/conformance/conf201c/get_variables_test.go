package conf201c

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetVariables201_RequestValidation(t *testing.T) {
	validator := validator201(t, "GetVariables", "request")
	component := component201()
	variable := variable201()

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						AttributeType: strPtr("Target"),
						Component:     component,
						Variable:      variable,
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid without attributeType",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						Component: component,
						Variable:  variable,
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid component without optional fields",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						Component: messages.ComponentType{Name: "component1"},
						Variable:  variable,
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid variable without optional fields",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						Component: component,
						Variable:  messages.VariableType{Name: "variable1"},
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "invalid missing getVariableData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown attributeType enum",
			Message: messages.GetVariablesRequest{
				GetVariableData: []messages.GetVariableDataType{
					{
						AttributeType: strPtr("invalidAttribute"),
						Component:     component,
						Variable:      variable,
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid getVariableData missing component",
			Message: map[string]any{
				"getVariableData": []any{
					map[string]any{
						"attributeType": "Target",
						"variable": map[string]any{
							"name":     "variable1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid getVariableData missing variable",
			Message: map[string]any{
				"getVariableData": []any{
					map[string]any{
						"attributeType": "Target",
						"component": map[string]any{
							"name":     "component1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty getVariableData",
			Message: map[string]any{
				"getVariableData": []any{},
			},
			Valid: false,
		},
		{
			Name: "invalid component evse id below minimum",
			Message: map[string]any{
				"getVariableData": []any{
					map[string]any{
						"component": map[string]any{
							"name": "component1",
							"evse": map[string]any{"id": -1},
						},
						"variable": map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetVariables201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "GetVariables", "response")
	component := component201()
	variable := variable201()

	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeType:   strPtr("Target"),
						AttributeValue:  strPtr("dummyValue"),
						Component:       component,
						Variable:        variable,
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid without attributeValue",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeType:   strPtr("Target"),
						Component:       component,
						Variable:        variable,
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid without attributeType",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						Component:       component,
						Variable:        variable,
					},
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing attributeStatus",
			Message: map[string]any{
				"getVariableResult": []any{
					map[string]any{
						"component": map[string]any{
							"name":     "component1",
							"instance": "instance1",
						},
						"variable": map[string]any{
							"name":     "variable1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component",
			Message: map[string]any{
				"getVariableResult": []any{
					map[string]any{
						"attributeStatus": "Accepted",
						"variable": map[string]any{
							"name":     "variable1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable",
			Message: map[string]any{
				"getVariableResult": []any{
					map[string]any{
						"attributeStatus": "Accepted",
						"component": map[string]any{
							"name":     "component1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing getVariableResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown attributeType enum",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeType:   strPtr("invalidAttribute"),
						AttributeValue:  strPtr("dummyValue"),
						Component:       component,
						Variable:        variable,
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid unknown attributeStatus enum",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "invalidStatus",
						AttributeType:   strPtr("Target"),
						AttributeValue:  strPtr("dummyValue"),
						Component:       component,
						Variable:        variable,
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid attributeValue exceeds maxLength 2500",
			Message: messages.GetVariablesResponse{
				GetVariableResult: []messages.GetVariableResultType{
					{
						AttributeStatus: "Accepted",
						AttributeType:   strPtr("Target"),
						AttributeValue:  strPtr(strings.Repeat("x", 2501)),
						Component:       component,
						Variable:        variable,
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty getVariableResult",
			Message: map[string]any{
				"getVariableResult": []any{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetVariables201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.GetVariables)
}
