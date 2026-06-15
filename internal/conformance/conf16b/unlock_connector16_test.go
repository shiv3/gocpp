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
	"github.com/stretchr/testify/require"
)

func TestUnlockConnector16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "UnlockConnector", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.UnlockConnectorRequest{
				ConnectorID: 1,
			},
			Valid: true,
		},
		// TODO(parity): needs schema override for connectorId minimum.
		{
			Name:    "invalid missing connectorId",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnlockConnector16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "UnlockConnector", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid unlocked response",
			Message: messages.UnlockConnectorResponse{
				Status: messages.UnlockConnectorResponseStatusUnlocked,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.UnlockConnectorResponse{
				Status: messages.UnlockConnectorResponseStatus("invalidUnlockStatus"),
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

func TestUnlockConnector16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.UnlockConnectorRequest, messages.UnlockConnectorResponse]{
		Action:    v16profiles.UnlockConnector.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.UnlockConnectorRequest) (messages.UnlockConnectorResponse, error) {
		return messages.UnlockConnectorResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
