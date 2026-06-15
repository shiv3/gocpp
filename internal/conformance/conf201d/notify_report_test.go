package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func variableAttribute201() messages.VariableAttributeType {
	return messages.VariableAttributeType{
		Type:       ptr("Actual"),
		Value:      ptr("someValue"),
		Mutability: ptr("ReadWrite"),
		Persistent: ptr(false),
		Constant:   ptr(false),
	}
}

func variableCharacteristics201(dataType string) *messages.VariableCharacteristicsType {
	return &messages.VariableCharacteristicsType{
		Unit:               ptr("KWh"),
		DataType:           dataType,
		MinLimit:           ptr(dec("1.0")),
		MaxLimit:           ptr(dec("22.0")),
		ValuesList:         ptr("7.0"),
		SupportsMonitoring: true,
	}
}

func reportData201() messages.ReportDataType {
	return messages.ReportDataType{
		Component:               messages.ComponentType{Name: "component1"},
		Variable:                messages.VariableType{Name: "variable1"},
		VariableAttribute:       []messages.VariableAttributeType{variableAttribute201()},
		VariableCharacteristics: variableCharacteristics201("string"),
	}
}

func TestNotifyReport201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid required fields only",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				SeqNo:       0,
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				Tbc:         ptr(true),
				SeqNo:       0,
				ReportData:  []messages.ReportDataType{reportData201()},
			},
			Valid: true,
		},
		{
			Name: "valid report data without characteristics",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				Tbc:         ptr(true),
				SeqNo:       0,
				ReportData: []messages.ReportDataType{
					{
						Component:         messages.ComponentType{Name: "comp1"},
						Variable:          messages.VariableType{Name: "var1"},
						VariableAttribute: []messages.VariableAttributeType{variableAttribute201()},
					},
				},
			},
			Valid: true,
		},
		{
			Name: "valid zero seqNo",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				Tbc:         ptr(true),
				ReportData:  []messages.ReportDataType{reportData201()},
			},
			Valid: true,
		},
		{
			Name: "valid without tbc",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				ReportData:  []messages.ReportDataType{reportData201()},
			},
			Valid: true,
		},
		{
			Name: "valid empty reportData omitted",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				ReportData:  []messages.ReportDataType{},
			},
			Valid: true,
		},
		{
			Name: "valid without reportData",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.NotifyReportRequest{
				GeneratedAt: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing component name",
			Message: map[string]any{
				"requestId":   42,
				"generatedAt": fixedTime201(),
				"tbc":         true,
				"seqNo":       0,
				"reportData": []any{
					map[string]any{
						"component": map[string]any{},
						"variable":  map[string]any{"name": "var1"},
						"variableAttribute": []any{
							map[string]any{"value": "someValue"},
						},
						"variableCharacteristics": map[string]any{"dataType": "string", "supportsMonitoring": true},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable name",
			Message: map[string]any{
				"requestId":   42,
				"generatedAt": fixedTime201(),
				"tbc":         true,
				"seqNo":       0,
				"reportData": []any{
					map[string]any{
						"component": map[string]any{"name": "comp1"},
						"variable":  map[string]any{},
						"variableAttribute": []any{
							map[string]any{"value": "someValue"},
						},
						"variableCharacteristics": map[string]any{"dataType": "string", "supportsMonitoring": true},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid empty variableAttribute",
			Message: map[string]any{
				"requestId":   42,
				"generatedAt": fixedTime201(),
				"tbc":         true,
				"seqNo":       0,
				"reportData": []any{
					map[string]any{
						"component":               map[string]any{"name": "comp1"},
						"variable":                map[string]any{"name": "var1"},
						"variableAttribute":       []any{},
						"variableCharacteristics": map[string]any{"dataType": "string", "supportsMonitoring": true},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid too many variableAttribute entries",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				Tbc:         ptr(true),
				SeqNo:       0,
				ReportData: []messages.ReportDataType{
					{
						Component: messages.ComponentType{Name: "comp1"},
						Variable:  messages.VariableType{Name: "var1"},
						VariableAttribute: []messages.VariableAttributeType{
							variableAttribute201(),
							variableAttribute201(),
							variableAttribute201(),
							variableAttribute201(),
							variableAttribute201(),
						},
						VariableCharacteristics: variableCharacteristics201("string"),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid variableCharacteristics dataType enum",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				Tbc:         ptr(true),
				SeqNo:       0,
				ReportData: []messages.ReportDataType{
					{
						Component:               messages.ComponentType{Name: "comp1"},
						Variable:                messages.VariableType{Name: "var1"},
						VariableAttribute:       []messages.VariableAttributeType{variableAttribute201()},
						VariableCharacteristics: variableCharacteristics201("unknownType"),
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid variableAttribute mutability enum",
			Message: messages.NotifyReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
				Tbc:         ptr(true),
				SeqNo:       0,
				ReportData: []messages.ReportDataType{
					{
						Component: messages.ComponentType{Name: "comp1"},
						Variable:  messages.VariableType{Name: "var1"},
						VariableAttribute: []messages.VariableAttributeType{
							{Mutability: ptr("invalidMutability")},
						},
						VariableCharacteristics: variableCharacteristics201("string"),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid requestId below minimum")
	skipSchemaOverride201(t, "invalid seqNo below minimum")
}

func TestNotifyReport201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.NotifyReportResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyReport201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyReport)
}
