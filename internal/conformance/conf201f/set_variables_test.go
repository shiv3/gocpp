package conf201f

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func setVariableData201() messages.SetVariableDataType {
	return messages.SetVariableDataType{
		AttributeType:  ptr("Target"),
		AttributeValue: "dummyValue",
		Component:      component201(),
		Variable:       variable201(),
	}
}

func setVariableResult201(status string) messages.SetVariableResultType {
	return messages.SetVariableResultType{
		AttributeType:       ptr("Target"),
		AttributeStatus:     status,
		Component:           component201(),
		Variable:            variable201(),
		AttributeStatusInfo: statusInfo201("200"),
	}
}

func TestSetVariables201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetVariables", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{setVariableData201()},
			},
			Valid: true,
		},
		{
			Name: "valid without attributeType",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := setVariableData201()
						data.AttributeType = nil
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid component name only",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := setVariableData201()
						data.Component = messages.ComponentType{Name: "component1"}
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid variable name only",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := setVariableData201()
						data.Variable = messages.VariableType{Name: "variable1"}
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing attributeValue",
			Message: map[string]any{
				"setVariableData": []any{
					map[string]any{
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component",
			Message: map[string]any{
				"setVariableData": []any{
					map[string]any{
						"attributeValue": "dummyValue",
						"variable":       map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable",
			Message: map[string]any{
				"setVariableData": []any{
					map[string]any{
						"attributeValue": "dummyValue",
						"component":      map[string]any{"name": "component1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty setVariableData",
			Message: map[string]any{
				"setVariableData": []any{},
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid attributeType enum",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := setVariableData201()
						data.AttributeType = ptr("invalidAttribute")
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid attributeValue exceeds maxLength 1000",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := setVariableData201()
						data.AttributeValue = longString(1001)
						return data
					}(),
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid component.evse.id below minimum")
}

func TestSetVariables201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetVariables", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{setVariableResult201("Accepted")},
			},
			Valid: true,
		},
		{
			Name: "valid without statusInfo",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					func() messages.SetVariableResultType {
						result := setVariableResult201("Accepted")
						result.AttributeStatusInfo = nil
						return result
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid without attributeType",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					func() messages.SetVariableResultType {
						result := setVariableResult201("Accepted")
						result.AttributeType = nil
						result.AttributeStatusInfo = nil
						return result
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing attributeStatus",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"attributeStatus": "Accepted",
						"component":       map[string]any{"name": "component1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"attributeStatus": "Accepted",
						"variable":        map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty setVariableResult",
			Message: map[string]any{
				"setVariableResult": []any{},
			},
			Valid: false,
		},
		{
			Name:    "invalid empty response",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid attributeType enum",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					func() messages.SetVariableResultType {
						result := setVariableResult201("Accepted")
						result.AttributeType = ptr("invalidAttribute")
						return result
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid attributeStatus enum",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{setVariableResult201("invalidStatus")},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component name",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"attributeType":   "Target",
						"attributeStatus": "Accepted",
						"component":       map[string]any{},
						"variable":        map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable name",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"attributeType":   "Target",
						"attributeStatus": "Accepted",
						"component":       map[string]any{"name": "component1"},
						"variable":        map[string]any{},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"attributeType":       "Target",
						"attributeStatus":     "Accepted",
						"component":           map[string]any{"name": "component1"},
						"variable":            map[string]any{"name": "variable1"},
						"attributeStatusInfo": map[string]any{},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariables201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.SetVariables)
}
