package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestClearChargingProfile21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "ClearChargingProfile", "request")

	purpose := "ChargingStationMaxProfile"
	invalidPurpose := "InvalidChargingProfilePurpose"
	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					ChargingProfilePurpose: &purpose,
					EVSEID:                 int32Ptr(1),
					StackLevel:             int32Ptr(1),
				},
				ChargingProfileID: int32Ptr(1),
			},
			Valid: true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.ClearChargingProfileRequest{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum chargingProfilePurpose",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					ChargingProfilePurpose: &invalidPurpose,
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearChargingProfile21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "ClearChargingProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearChargingProfileResponse{
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
			Message: messages.ClearChargingProfileResponse{
				Status:     "Accepted",
				StatusInfo: invalidStatusInfoReason(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.ClearChargingProfileResponse{
				Status: "InvalidClearChargingProfileStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearChargingProfile21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.ClearChargingProfile)
}
