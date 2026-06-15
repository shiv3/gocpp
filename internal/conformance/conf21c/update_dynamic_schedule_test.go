package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestUpdateDynamicSchedule21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "UpdateDynamicSchedule", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UpdateDynamicScheduleRequest{
				ChargingProfileID: 1,
				ScheduleUpdate: messages.ChargingScheduleUpdateType{
					Limit: ptr(dec21("10.0")),
				},
			},
			Valid: true,
		},
		{
			Name: "missing chargingProfileId",
			Message: map[string]any{
				"scheduleUpdate": map[string]any{
					"limit": 10.0,
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.UpdateDynamicScheduleRequest{
				ChargingProfileID: 1,
				CustomData:        &messages.CustomDataType{VendorID: longString(256)},
				ScheduleUpdate: messages.ChargingScheduleUpdateType{
					Limit: ptr(dec21("10.0")),
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateDynamicSchedule21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "UpdateDynamicSchedule", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UpdateDynamicScheduleResponse{
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
			Name: "exceeds maxLength",
			Message: messages.UpdateDynamicScheduleResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo21(longString(21)),
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.UpdateDynamicScheduleResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateDynamicSchedule21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.UpdateDynamicSchedule)
}
