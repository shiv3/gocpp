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

func TestClearDisplayMessage201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearDisplayMessage", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid display message id",
			Message: messages.ClearDisplayMessageRequest{
				ID: 42,
			},
			Valid: true,
		},
		{
			Name:    "valid zero-value request",
			Message: messages.ClearDisplayMessageRequest{},
			Valid:   true,
		},
		{
			Name: "valid full request",
			Message: messages.ClearDisplayMessageRequest{
				CustomData: testCustomData(),
				ID:         42,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing id",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearDisplayMessage201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearDisplayMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.ClearDisplayMessageResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid unknown response",
			Message: messages.ClearDisplayMessageResponse{
				Status: "Unknown",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.ClearDisplayMessageResponse{
				CustomData: testCustomData(),
				Status:     "Accepted",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.ClearDisplayMessageResponse{
				Status: "BadEnum",
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

func TestClearDisplayMessage201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.ClearDisplayMessageRequest, messages.ClearDisplayMessageResponse]{
		Action:    v201profiles.ClearDisplayMessage.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ClearDisplayMessageRequest) (messages.ClearDisplayMessageResponse, error) {
		return messages.ClearDisplayMessageResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
