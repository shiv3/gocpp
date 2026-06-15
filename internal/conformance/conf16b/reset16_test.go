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

func TestReset16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "Reset", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid hard reset",
			Message: messages.ResetRequest{
				Type: messages.ResetRequestTypeHard,
			},
			Valid: true,
		},
		{
			Name: "valid soft reset",
			Message: messages.ResetRequest{
				Type: messages.ResetRequestTypeSoft,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown type enum",
			Message: messages.ResetRequest{
				Type: messages.ResetRequestType("invalidResetType"),
			},
			Valid: false,
		},
		{
			Name:    "invalid missing type",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReset16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "Reset", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.ResetResponse{
				Status: messages.ResetResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.ResetResponse{
				Status: messages.ResetResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.ResetResponse{
				Status: messages.ResetResponseStatus("invalidResetStatus"),
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

func TestReset16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.ResetRequest, messages.ResetResponse]{
		Action:    v16profiles.Reset.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ResetRequest) (messages.ResetResponse, error) {
		return messages.ResetResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
