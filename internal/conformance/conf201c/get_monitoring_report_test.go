package conf201c

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetMonitoringReport201_RequestValidation(t *testing.T) {
	validator := validator201(t, "GetMonitoringReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.GetMonitoringReportRequest{
				RequestID:          42,
				MonitoringCriteria: []string{"ThresholdMonitoring", "DeltaMonitoring", "PeriodicMonitoring"},
				ComponentVariable:  []messages.ComponentVariableType{componentVariable201()},
			},
			Valid: true,
		},
		{
			Name: "valid no optional fields",
			Message: messages.GetMonitoringReportRequest{
				RequestID: 42,
			},
			Valid: true,
		},
		{
			Name:    "valid zero requestId",
			Message: messages.GetMonitoringReportRequest{},
			Valid:   true,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"monitoringCriteria": []string{"ThresholdMonitoring"},
			},
			Valid: false,
		},
		{
			Name: "invalid too many monitoringCriteria items",
			Message: messages.GetMonitoringReportRequest{
				RequestID:          42,
				MonitoringCriteria: []string{"ThresholdMonitoring", "DeltaMonitoring", "PeriodicMonitoring", "ThresholdMonitoring"},
			},
			Valid: false,
		},
		{
			Name: "invalid unknown monitoringCriteria enum",
			Message: messages.GetMonitoringReportRequest{
				RequestID:          42,
				MonitoringCriteria: []string{"invalidMonitoringCriteria"},
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable missing component",
			Message: map[string]any{
				"requestId": 42,
				"componentVariable": []any{
					map[string]any{
						"variable": map[string]any{
							"name":     "variable1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for requestId minimum and empty array minItems parity.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetMonitoringReport201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "GetMonitoringReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.GetMonitoringReportResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.GetMonitoringReportResponse{
				Status: "invalidDeviceModelStatus",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetMonitoringReport201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.GetMonitoringReport)
}
