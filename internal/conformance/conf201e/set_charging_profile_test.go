package conf201e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestSetChargingProfile201_RequestValidation(t *testing.T) {
	useDecimalJSONWithoutQuotes201e(t)

	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetChargingProfile", "request")

	chargingProfile := testChargingProfile201e(0)
	cases := []conformance.ValidationCase{
		{
			Name: "valid request with evseId",
			Message: messages.SetChargingProfileRequest{
				EVSEID:          1,
				ChargingProfile: chargingProfile,
			},
			Valid: true,
		},
		{
			Name: "valid zero evseId request",
			Message: messages.SetChargingProfileRequest{
				ChargingProfile: chargingProfile,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing evseId",
			Message: map[string]any{
				"chargingProfile": chargingProfile,
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingProfile",
			Message: map[string]any{
				"evseId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid chargingProfile stackLevel below minimum",
			Message: messages.SetChargingProfileRequest{
				EVSEID:          1,
				ChargingProfile: testChargingProfile201e(-1),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetChargingProfile201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetChargingProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SetChargingProfileResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SetChargingProfileResponse{
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
			Message: messages.SetChargingProfileResponse{
				Status: "invalidChargingProfileStatus",
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

func TestSetChargingProfile201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.SetChargingProfile)
}
