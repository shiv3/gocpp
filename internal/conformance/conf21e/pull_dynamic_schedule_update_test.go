package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestPullDynamicScheduleUpdate21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "PullDynamicScheduleUpdate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.PullDynamicScheduleUpdateRequest{
				ChargingProfileID: 1,
			},
			Valid: true,
		},
		{
			Name:    "missing chargingProfileId",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPullDynamicScheduleUpdate21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "PullDynamicScheduleUpdate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.PullDynamicScheduleUpdateResponse{
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
			Name: "exceeds maxLength statusInfo reasonCode",
			Message: messages.PullDynamicScheduleUpdateResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.PullDynamicScheduleUpdateResponse{
				Status: "invalidChargingProfileStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestPullDynamicScheduleUpdate21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.PullDynamicScheduleUpdate)
}
