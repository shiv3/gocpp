package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestPullDynamicScheduleUpdate21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
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
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
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
