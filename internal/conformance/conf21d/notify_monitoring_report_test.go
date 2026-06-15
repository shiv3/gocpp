package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyMonitoringReport21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyMonitoringReport", "request")

	longComponent := testMonitoringData()
	longComponent.Component = messages.ComponentType{Name: longString(51)}

	invalidMonitorType := testVariableMonitoring()
	invalidMonitorType.Type = "InvalidMonitorType"

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyMonitoringReportRequest{
				GeneratedAt: testTime(),
				Monitor:     []messages.MonitoringDataType{testMonitoringData()},
				RequestID:   1,
				SeqNo:       1,
			},
			Valid: true,
		},
		{
			Name: "missing requestId",
			Message: map[string]any{
				"generatedAt": testTime(),
				"seqNo":       1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength component.name",
			Message: messages.NotifyMonitoringReportRequest{
				GeneratedAt: testTime(),
				Monitor:     []messages.MonitoringDataType{longComponent},
				RequestID:   1,
				SeqNo:       1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum variableMonitoring.type",
			Message: messages.NotifyMonitoringReportRequest{
				GeneratedAt: testTime(),
				Monitor: []messages.MonitoringDataType{
					{
						Component:          testComponent(),
						Variable:           testVariable(),
						VariableMonitoring: []messages.VariableMonitoringType{invalidMonitorType},
					},
				},
				RequestID: 1,
				SeqNo:     1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyMonitoringReport21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyMonitoringReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyMonitoringReportResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.NotifyMonitoringReportResponse{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyMonitoringReport21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.NotifyMonitoringReport)
}
