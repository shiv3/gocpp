package conf21c

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyChargingLimit21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyChargingLimit", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyChargingLimitRequest{
				ChargingLimit: messages.ChargingLimitType{
					ChargingLimitSource: "CSMS",
				},
				ChargingSchedule: []messages.ChargingScheduleType{chargingSchedule21()},
			},
			Valid: true,
		},
		{
			Name:    "missing chargingLimit",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.NotifyChargingLimitRequest{
				ChargingLimit: messages.ChargingLimitType{
					ChargingLimitSource: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.NotifyChargingLimitRequest{
				ChargingLimit: messages.ChargingLimitType{
					ChargingLimitSource: "CSMS",
				},
				ChargingSchedule: []messages.ChargingScheduleType{
					{
						ChargingRateUnit:       "BogusUnit",
						ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{chargingSchedulePeriod21()},
						ID:                     1,
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyChargingLimit21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyChargingLimit", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyChargingLimitResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.NotifyChargingLimitResponse{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyChargingLimit21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifyChargingLimit)
}
