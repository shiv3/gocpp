package conf201a

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestClearedChargingLimit201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearedChargingLimit", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid with evseId",
			Message: messages.ClearedChargingLimitRequest{
				ChargingLimitSource: "EMS",
				EVSEID:              int32Ptr(0),
			},
			Valid: true,
		},
		{
			Name: "valid source only",
			Message: messages.ClearedChargingLimitRequest{
				ChargingLimitSource: "EMS",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.ClearedChargingLimitRequest{
				ChargingLimitSource: "CSO",
				CustomData:          testCustomData(),
				EVSEID:              int32Ptr(1),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing chargingLimitSource",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid chargingLimitSource enum",
			Message: messages.ClearedChargingLimitRequest{
				ChargingLimitSource: "BadEnum",
			},
			Valid: false,
		},
		// TODO(parity): needs schema override; OCA schema has no minimum for evseId.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearedChargingLimit201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearedChargingLimit", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.ClearedChargingLimitResponse{},
			Valid:   true,
		},
		{
			Name: "valid full response",
			Message: messages.ClearedChargingLimitResponse{
				CustomData: testCustomData(),
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearedChargingLimit201_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.ClearedChargingLimitRequest, messages.ClearedChargingLimitResponse]{
		Action:    v201profiles.ClearedChargingLimit.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.ClearedChargingLimitRequest) (messages.ClearedChargingLimitResponse, error) {
		return messages.ClearedChargingLimitResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
