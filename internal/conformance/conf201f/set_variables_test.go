package conf201f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func testSetVariableData201f() messages.SetVariableDataType {
	return messages.SetVariableDataType{
		AttributeType:  strPtr201f("Target"),
		AttributeValue: "dummyValue",
		Component:      testComponent201f(),
		Variable:       testVariable201f(),
	}
}

func testSetVariableResult201f(status string) messages.SetVariableResultType {
	return messages.SetVariableResultType{
		AttributeType:       strPtr201f("Target"),
		AttributeStatus:     status,
		Component:           testComponent201f(),
		Variable:            testVariable201f(),
		AttributeStatusInfo: testStatusInfo201f(),
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
				SetVariableData: []messages.SetVariableDataType{testSetVariableData201f()},
			},
			Valid: true,
		},
		{
			Name: "valid without attributeType",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := testSetVariableData201f()
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
						data := testSetVariableData201f()
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
						data := testSetVariableData201f()
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
			Name:    "invalid missing setVariableData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid attributeType enum",
			Message: messages.SetVariablesRequest{
				SetVariableData: []messages.SetVariableDataType{
					func() messages.SetVariableDataType {
						data := testSetVariableData201f()
						data.AttributeType = strPtr201f("invalidAttribute")
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
						data := testSetVariableData201f()
						data.AttributeValue = strings.Repeat("x", 1001)
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component with attributeType",
			Message: map[string]any{
				"setVariableData": []any{
					map[string]any{
						"attributeType":  "Target",
						"attributeValue": "dummyValue",
						"variable":       map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable with attributeType",
			Message: map[string]any{
				"setVariableData": []any{
					map[string]any{
						"attributeType":  "Target",
						"attributeValue": "dummyValue",
						"component":      map[string]any{"name": "component1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid component evse id below minimum",
			Message: map[string]any{
				"setVariableData": []any{
					map[string]any{
						"attributeValue": "dummyValue",
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

func TestSetVariables201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetVariables", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{testSetVariableResult201f("Accepted")},
			},
			Valid: true,
		},
		{
			Name: "valid without statusInfo",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					func() messages.SetVariableResultType {
						result := testSetVariableResult201f("Accepted")
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
						result := testSetVariableResult201f("Accepted")
						result.AttributeType = nil
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
			Name:    "invalid missing setVariableResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid attributeType enum",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{
					func() messages.SetVariableResultType {
						result := testSetVariableResult201f("Accepted")
						result.AttributeType = strPtr201f("invalidAttribute")
						return result
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid attributeStatus enum",
			Message: messages.SetVariablesResponse{
				SetVariableResult: []messages.SetVariableResultType{testSetVariableResult201f("invalidStatus")},
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
		{
			Name: "invalid empty attributeStatusInfo.reasonCode",
			Message: map[string]any{
				"setVariableResult": []any{
					map[string]any{
						"attributeStatus":     "Accepted",
						"component":           map[string]any{"name": "component1"},
						"variable":            map[string]any{"name": "variable1"},
						"attributeStatusInfo": map[string]any{"reasonCode": ""},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariables201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.SetVariables)
}
