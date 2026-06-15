package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestGetMonitoringReport21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetMonitoringReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetMonitoringReportRequest{
				ComponentVariable: []messages.ComponentVariableType{
					{
						Component: testComponent21(),
						Variable:  &messages.VariableType{Name: "Voltage"},
					},
				},
				MonitoringCriteria: []string{"ThresholdMonitoring"},
				RequestID:          1,
			},
			Valid: true,
		},
		{
			Name:    "missing requestId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength componentVariable.component.name",
			Message: messages.GetMonitoringReportRequest{
				ComponentVariable: []messages.ComponentVariableType{
					{
						Component: messages.ComponentType{Name: strings.Repeat("x", 51)},
					},
				},
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum monitoringCriteria",
			Message: messages.GetMonitoringReportRequest{
				MonitoringCriteria: []string{"InvalidCriteria"},
				RequestID:          1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetMonitoringReport21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetMonitoringReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetMonitoringReportResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.GetMonitoringReportResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.GetMonitoringReportResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetMonitoringReport21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetMonitoringReport)
}
