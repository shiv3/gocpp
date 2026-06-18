package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func variableMonitoring201(monitorType string) messages.VariableMonitoringType {
	return messages.VariableMonitoringType{
		ID:          1,
		Transaction: false,
		Value:       dec("42.42"),
		Type:        monitorType,
		Severity:    0,
	}
}

func monitoringData201(monitorType string) messages.MonitoringDataType {
	return messages.MonitoringDataType{
		Component:          messages.ComponentType{Name: "component1"},
		Variable:           messages.VariableType{Name: "variable1"},
		VariableMonitoring: []messages.VariableMonitoringType{variableMonitoring201(monitorType)},
	}
}

func TestNotifyMonitoringReport201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyMonitoringReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.NotifyMonitoringReportRequest{
				RequestID:   42,
				Tbc:         ptr(true),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				Monitor:     []messages.MonitoringDataType{monitoringData201("Periodic")},
			},
			Valid: true,
		},
		{
			Name: "valid empty monitor omitted",
			Message: messages.NotifyMonitoringReportRequest{
				RequestID:   42,
				Tbc:         ptr(true),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				Monitor:     []messages.MonitoringDataType{},
			},
			Valid: true,
		},
		{
			Name: "valid without monitor",
			Message: messages.NotifyMonitoringReportRequest{
				RequestID:   42,
				Tbc:         ptr(true),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name: "valid without tbc",
			Message: messages.NotifyMonitoringReportRequest{
				RequestID:   42,
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name: "valid zero seqNo",
			Message: messages.NotifyMonitoringReportRequest{
				RequestID:   42,
				GeneratedAt: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.NotifyMonitoringReportRequest{
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
			Name: "invalid variableMonitoring type enum",
			Message: messages.NotifyMonitoringReportRequest{
				RequestID:   42,
				Tbc:         ptr(true),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				Monitor:     []messages.MonitoringDataType{monitoringData201("invalidMonitorType")},
			},
			Valid: false,
		},
		{
			Name: "invalid empty variableMonitoring",
			Message: map[string]any{
				"requestId":   42,
				"tbc":         true,
				"seqNo":       0,
				"generatedAt": fixedTime201(),
				"monitor": []any{
					map[string]any{
						"component":          map[string]any{"name": "component1"},
						"variable":           map[string]any{"name": "variable1"},
						"variableMonitoring": []any{},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing variable name",
			Message: map[string]any{
				"requestId":   42,
				"tbc":         true,
				"seqNo":       0,
				"generatedAt": fixedTime201(),
				"monitor": []any{
					map[string]any{
						"component": map[string]any{"name": "component1"},
						"variable":  map[string]any{},
						"variableMonitoring": []any{
							map[string]any{
								"id":          1,
								"transaction": false,
								"value":       42.42,
								"type":        "Periodic",
								"severity":    0,
							},
						},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid requestId below minimum")
	skipSchemaOverride201(t, "invalid seqNo below minimum")
	skipSchemaOverride201(t, "invalid variableMonitoring.id below minimum")
	skipSchemaOverride201(t, "invalid variableMonitoring.severity below minimum")
	skipSchemaOverride201(t, "invalid variableMonitoring.severity above maximum")
}

func TestNotifyMonitoringReport201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyMonitoringReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.NotifyMonitoringReportResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyMonitoringReport201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyMonitoringReport)
}
