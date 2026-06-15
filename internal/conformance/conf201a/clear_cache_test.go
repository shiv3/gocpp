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

func TestClearCache201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearCache", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty request",
			Message: messages.ClearCacheRequest{},
			Valid:   true,
		},
		{
			Name: "valid full request",
			Message: messages.ClearCacheRequest{
				CustomData: testCustomData(),
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearCache201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ClearCache", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted with statusInfo",
			Message: messages.ClearCacheResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.ClearCacheResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.ClearCacheResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.ClearCacheResponse{
				CustomData: testCustomData(),
				Status:     "Accepted",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.ClearCacheResponse{
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
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearCache201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.ClearCacheRequest, messages.ClearCacheResponse]{
		Action:    v201profiles.ClearCache.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ClearCacheRequest) (messages.ClearCacheResponse, error) {
		return messages.ClearCacheResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
