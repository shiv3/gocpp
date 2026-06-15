package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSetVariableMonitoring21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "SetVariableMonitoring", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetVariableMonitoringRequest{
				SetMonitoringData: []messages.SetMonitoringDataType{setMonitoringData21()},
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "SetVariableMonitoring", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariableMonitoring21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "SetVariableMonitoring", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetVariableMonitoringResponse{
				SetMonitoringResult: []messages.SetMonitoringResultType{setMonitoringResult21()},
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "SetVariableMonitoring", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetVariableMonitoring21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.SetVariableMonitoring)
}
