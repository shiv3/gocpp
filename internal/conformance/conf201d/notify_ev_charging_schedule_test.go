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

func chargingScheduleMap201() map[string]any {
	return map[string]any{
		"id":               1,
		"startSchedule":    fixedTime201(),
		"duration":         600,
		"chargingRateUnit": "W",
		"minChargingRate":  6.0,
		"chargingSchedulePeriod": []any{
			map[string]any{
				"startPeriod": 0,
				"limit":       10.0,
			},
		},
	}
}

func TestNotifyEVChargingSchedule201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyEVChargingSchedule", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.NotifyEVChargingScheduleRequest{
				TimeBase:         fixedTime201(),
				EVSEID:           1,
				ChargingSchedule: chargingSchedule201(),
			},
			Valid: true,
		},
		{
			Name: "invalid missing chargingSchedule",
			Message: map[string]any{
				"timeBase": fixedTime201(),
				"evseId":   1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing evseId",
			Message: map[string]any{
				"timeBase":         fixedTime201(),
				"chargingSchedule": chargingScheduleMap201(),
			},
			Valid: false,
		},
		{
			Name: "invalid missing timeBase",
			Message: map[string]any{
				"evseId":           1,
				"chargingSchedule": chargingScheduleMap201(),
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid chargingSchedule.chargingRateUnit enum",
			Message: messages.NotifyEVChargingScheduleRequest{
				TimeBase: fixedTime201(),
				EVSEID:   1,
				ChargingSchedule: messages.ChargingScheduleType{
					ID:                     1,
					ChargingRateUnit:       "invalidStruct",
					ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{chargingSchedulePeriod201()},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid evseId below minimum")
}

func TestNotifyEVChargingSchedule201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyEVChargingSchedule", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.NotifyEVChargingScheduleResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo201("ok"),
			},
			Valid: true,
		},
		{
			Name: "valid rejected response with statusInfo",
			Message: messages.NotifyEVChargingScheduleResponse{
				Status:     "Rejected",
				StatusInfo: statusInfo201("ok"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.NotifyEVChargingScheduleResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.NotifyEVChargingScheduleResponse{
				Status: "invalidStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEVChargingSchedule201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyEVChargingSchedule)
}
