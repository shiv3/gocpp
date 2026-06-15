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

func TestGetChargingProfiles21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetChargingProfiles", "request")

	purpose := "ChargingStationMaxProfile"
	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetChargingProfilesRequest{
				ChargingProfile: messages.ChargingProfileCriterionType{
					ChargingLimitSource:    []string{"CSMS"},
					ChargingProfileID:      []int32{1},
					ChargingProfilePurpose: &purpose,
					StackLevel:             int32Ptr21(0),
				},
				EVSEID:    int32Ptr21(1),
				RequestID: 1,
			},
			Valid: true,
		},
		{
			Name: "missing requestId",
			Message: map[string]any{
				"chargingProfile": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength chargingProfile.chargingLimitSource",
			Message: messages.GetChargingProfilesRequest{
				ChargingProfile: messages.ChargingProfileCriterionType{
					ChargingLimitSource: []string{strings.Repeat("x", 21)},
				},
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum chargingProfile.chargingProfilePurpose",
			Message: messages.GetChargingProfilesRequest{
				ChargingProfile: messages.ChargingProfileCriterionType{
					ChargingProfilePurpose: stringPtr21("InvalidPurpose"),
				},
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetChargingProfiles21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetChargingProfiles", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetChargingProfilesResponse{
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
			Message: messages.GetChargingProfilesResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.GetChargingProfilesResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetChargingProfiles21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetChargingProfiles)
}
