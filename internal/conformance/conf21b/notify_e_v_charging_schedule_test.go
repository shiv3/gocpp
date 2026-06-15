package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyEVChargingSchedule21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyEVChargingSchedule", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyEVChargingScheduleRequest{
				ChargingSchedule: chargingSchedule21(),
				EVSEID:           1,
				TimeBase:         fixedTime21(),
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "NotifyEVChargingSchedule", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEVChargingSchedule21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyEVChargingSchedule", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyEVChargingScheduleResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "NotifyEVChargingSchedule", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEVChargingSchedule21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifyEVChargingSchedule)
}
