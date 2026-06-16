package conf16b

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func validChargingProfile16() *messages.ChargingProfile {
	return &messages.ChargingProfile{
		ChargingProfileID:      1,
		StackLevel:             1,
		ChargingProfilePurpose: messages.ChargingProfileChargingProfilePurposeChargePointMaxProfile,
		ChargingProfileKind:    messages.ChargingProfileChargingProfileKindAbsolute,
		ChargingSchedule: messages.ChargingSchedule{
			ChargingRateUnit: messages.ChargingScheduleChargingRateUnitW,
			ChargingSchedulePeriod: []messages.ChargingSchedulePeriod{
				{
					StartPeriod: 0,
					Limit:       decimal.NewFromFloat(10.0),
				},
			},
		},
	}
}

func TestRemoteStartTransaction16_RequestValidation(t *testing.T) {
	oldDecimalJSON := decimal.MarshalJSONWithoutQuotes
	decimal.MarshalJSONWithoutQuotes = true
	t.Cleanup(func() {
		decimal.MarshalJSONWithoutQuotes = oldDecimalJSON
	})

	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "RemoteStartTransaction", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.RemoteStartTransactionRequest{
				IDTag:           "12345",
				ConnectorID:     ptr(int32(1)),
				ChargingProfile: validChargingProfile16(),
			},
			Valid: true,
		},
		{
			Name: "valid request with connectorId",
			Message: messages.RemoteStartTransactionRequest{
				IDTag:       "12345",
				ConnectorID: ptr(int32(1)),
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.RemoteStartTransactionRequest{
				IDTag: "12345",
			},
			Valid: true,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"idTag":       "12345",
				"connectorId": 0,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing idTag",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid idTag exceeds maxLength 20",
			Message: messages.RemoteStartTransactionRequest{
				IDTag:       ">20..................",
				ConnectorID: ptr(int32(1)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRemoteStartTransaction16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "RemoteStartTransaction", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.RemoteStartTransactionResponse{
				Status: messages.RemoteStartTransactionResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.RemoteStartTransactionResponse{
				Status: messages.RemoteStartTransactionResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.RemoteStartTransactionResponse{
				Status: messages.RemoteStartTransactionResponseStatus("invalidRemoteStartTransactionStatus"),
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRemoteStartTransaction16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.RemoteStartTransactionRequest, messages.RemoteStartTransactionResponse]{
		Action:    v16profiles.RemoteStartTransaction.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.RemoteStartTransactionRequest) (messages.RemoteStartTransactionResponse, error) {
		return messages.RemoteStartTransactionResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
