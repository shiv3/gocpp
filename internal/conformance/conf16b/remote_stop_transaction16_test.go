package conf16b

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestRemoteStopTransaction16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "RemoteStopTransaction", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.RemoteStopTransactionRequest{
				TransactionID: 1,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing transactionId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "valid negative transactionId",
			Message: messages.RemoteStopTransactionRequest{
				TransactionID: -1,
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRemoteStopTransaction16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "RemoteStopTransaction", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.RemoteStopTransactionResponse{
				Status: messages.RemoteStopTransactionResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.RemoteStopTransactionResponse{
				Status: messages.RemoteStopTransactionResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.RemoteStopTransactionResponse{
				Status: messages.RemoteStopTransactionResponseStatus("invalidRemoteStopTransactionStatus"),
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

func TestRemoteStopTransaction16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.RemoteStopTransactionRequest, messages.RemoteStopTransactionResponse]{
		Action:    v16profiles.RemoteStopTransaction.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.RemoteStopTransactionRequest) (messages.RemoteStopTransactionResponse, error) {
		return messages.RemoteStopTransactionResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
