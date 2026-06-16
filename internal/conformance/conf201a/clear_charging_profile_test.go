package conf201a

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestClearChargingProfile201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearChargingProfile", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileID: int32Ptr(1),
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					EVSEID:                 int32Ptr(1),
					ChargingProfilePurpose: strPtr("ChargingStationMaxProfile"),
					StackLevel:             int32Ptr(1),
				},
			},
			Valid: true,
		},
		{
			Name: "valid without stackLevel",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileID: int32Ptr(1),
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					EVSEID:                 int32Ptr(1),
					ChargingProfilePurpose: strPtr("ChargingStationMaxProfile"),
				},
			},
			Valid: true,
		},
		{
			Name: "valid evse criteria",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileID: int32Ptr(1),
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					EVSEID: int32Ptr(1),
				},
			},
			Valid: true,
		},
		{
			Name: "valid criteria without chargingProfileId",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					EVSEID: int32Ptr(1),
				},
			},
			Valid: true,
		},
		{
			Name: "valid empty criteria",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileCriteria: &messages.ClearChargingProfileType{},
			},
			Valid: true,
		},
		{
			Name:    "valid empty request",
			Message: messages.ClearChargingProfileRequest{},
			Valid:   true,
		},
		{
			Name: "invalid chargingProfilePurpose enum",
			Message: messages.ClearChargingProfileRequest{
				ChargingProfileCriteria: &messages.ClearChargingProfileType{
					ChargingProfilePurpose: strPtr("BadEnum"),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid criteria evseId below minimum",
			Message: map[string]any{
				"chargingProfileCriteria": map[string]any{"evseId": -1},
			},
			Valid: false,
		},
		{
			Name: "invalid criteria stackLevel below minimum",
			Message: map[string]any{
				"chargingProfileCriteria": map[string]any{"stackLevel": 0},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearChargingProfile201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearChargingProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted with statusInfo",
			Message: messages.ClearChargingProfileResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.ClearChargingProfileResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid unknown response",
			Message: messages.ClearChargingProfileResponse{
				Status: "Unknown",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.ClearChargingProfileResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearChargingProfile201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.ClearChargingProfileRequest, messages.ClearChargingProfileResponse]{
		Action:    v201profiles.ClearChargingProfile.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ClearChargingProfileRequest) (messages.ClearChargingProfileResponse, error) {
		return messages.ClearChargingProfileResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
