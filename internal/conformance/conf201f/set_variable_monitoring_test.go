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

func testSetMonitoringData201f(monitorType string) messages.SetMonitoringDataType {
	return messages.SetMonitoringDataType{
		ID:          int32Ptr201f(2),
		Transaction: boolPtr201f(true),
		Value:       decimal201f("42.0"),
		Type:        monitorType,
		Severity:    5,
		Component:   messages.ComponentType{Name: "component1"},
		Variable:    messages.VariableType{Name: "variable1"},
	}
}

func testSetMonitoringResult201f(status, monitorType string) messages.SetMonitoringResultType {
	return messages.SetMonitoringResultType{
		ID:         int32Ptr201f(2),
		Status:     status,
		Type:       monitorType,
		Severity:   5,
		Component:  messages.ComponentType{Name: "component1"},
		Variable:   messages.VariableType{Name: "variable1"},
		StatusInfo: testStatusInfo201f(),
	}
}

func TestSetVariableMonitoring201_RequestValidation(t *testing.T) {
	useDecimalJSONWithoutQuotes201f(t)

	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetVariableMonitoring", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{testSetMonitoringData201f("UpperThreshold")},
			},
			Valid: true,
		},
		{
			Name: "valid without id",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{
					func() messages.SetMonitoringDataType {
						data := testSetMonitoringData201f("UpperThreshold")
						data.ID = nil
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid without transaction",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{
					func() messages.SetMonitoringDataType {
						data := testSetMonitoringData201f("UpperThreshold")
						data.Transaction = nil
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid zero value",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{
					func() messages.SetMonitoringDataType {
						data := testSetMonitoringData201f("UpperThreshold")
						data.Value = decimal201f("0")
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid zero severity",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{
					func() messages.SetMonitoringDataType {
						data := testSetMonitoringData201f("UpperThreshold")
						data.Severity = 0
						return data
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing value",
			Message: map[string]any{
				"setMonitoringData": []any{
					map[string]any{
						"type":      "UpperThreshold",
						"severity":  5,
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing type",
			Message: map[string]any{
				"setMonitoringData": []any{
					map[string]any{
						"value":     42.0,
						"severity":  5,
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing severity",
			Message: map[string]any{
				"setMonitoringData": []any{
					map[string]any{
						"value":     42.0,
						"type":      "UpperThreshold",
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
				"setMonitoringData": []any{
					map[string]any{
						"value":    42.0,
						"type":     "UpperThreshold",
						"severity": 5,
						"variable": map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable",
			Message: map[string]any{
				"setMonitoringData": []any{
					map[string]any{
						"value":     42.0,
						"type":      "UpperThreshold",
						"severity":  5,
						"component": map[string]any{"name": "component1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty setMonitoringData object",
			Message: map[string]any{
				"setMonitoringData": []any{map[string]any{}},
			},
			Valid: false,
		},
		{
			Name: "invalid empty setMonitoringData",
			Message: map[string]any{
				"setMonitoringData": []any{},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing setMonitoringData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid type enum",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{testSetMonitoringData201f("invalidType")},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component and variable names",
			Message: map[string]any{
				"setMonitoringData": []any{
					map[string]any{
						"id":          2,
						"transaction": true,
						"value":       42.0,
						"type":        "UpperThreshold",
						"severity":    5,
						"component":   map[string]any{},
						"variable":    map[string]any{},
					},
				},
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for severity minimum.
		// TODO(parity): needs schema override for severity maximum.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariableMonitoring201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetVariableMonitoring", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{testSetMonitoringResult201f("Accepted", "UpperThreshold")},
			},
			Valid: true,
		},
		{
			Name: "valid without statusInfo",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{
					func() messages.SetMonitoringResultType {
						result := testSetMonitoringResult201f("Accepted", "UpperThreshold")
						result.StatusInfo = nil
						return result
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid without id",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{
					func() messages.SetMonitoringResultType {
						result := testSetMonitoringResult201f("Accepted", "UpperThreshold")
						result.ID = nil
						return result
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "valid zero severity",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{
					func() messages.SetMonitoringResultType {
						result := testSetMonitoringResult201f("Accepted", "UpperThreshold")
						result.Severity = 0
						result.StatusInfo = nil
						return result
					}(),
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing variable",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"status":    "Accepted",
						"type":      "UpperThreshold",
						"severity":  5,
						"component": map[string]any{"name": "component1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"status":   "Accepted",
						"type":     "UpperThreshold",
						"severity": 5,
						"variable": map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing type",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"status":    "Accepted",
						"severity":  5,
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing severity",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"status":    "Accepted",
						"type":      "UpperThreshold",
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing status",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"type":      "UpperThreshold",
						"severity":  5,
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty setMonitoringResult",
			Message: map[string]any{
				"setMonitoringResult": []any{},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing setMonitoringResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{testSetMonitoringResult201f("invalidStatus", "UpperThreshold")},
			},
			Valid: false,
		},
		{
			Name: "invalid type enum",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{testSetMonitoringResult201f("Accepted", "invalidType")},
			},
			Valid: false,
		},
		{
			Name: "invalid missing component name",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"id":         2,
						"status":     "Accepted",
						"type":       "UpperThreshold",
						"severity":   5,
						"component":  map[string]any{},
						"variable":   map[string]any{"name": "variable1"},
						"statusInfo": map[string]any{"reasonCode": "200"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable name",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"id":         2,
						"status":     "Accepted",
						"type":       "UpperThreshold",
						"severity":   5,
						"component":  map[string]any{"name": "component1"},
						"variable":   map[string]any{},
						"statusInfo": map[string]any{"reasonCode": "200"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"setMonitoringResult": []any{
					map[string]any{
						"id":         2,
						"status":     "Accepted",
						"type":       "UpperThreshold",
						"severity":   5,
						"component":  map[string]any{"name": "component1"},
						"variable":   map[string]any{"name": "variable1"},
						"statusInfo": map[string]any{},
					},
				},
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for severity minimum.
		// TODO(parity): needs schema override for severity maximum.
		// TODO(parity): needs schema override for empty statusInfo.reasonCode minLength.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariableMonitoring201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.SetVariableMonitoring)
}
