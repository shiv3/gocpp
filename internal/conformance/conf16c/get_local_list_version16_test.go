package conf16c

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestGetLocalListVersion16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "GetLocalListVersion", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty request",
			Message: messages.GetLocalListVersionRequest{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLocalListVersion16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "GetLocalListVersion", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid positive listVersion",
			Message: messages.GetLocalListVersionResponse{
				ListVersion: 1,
			},
			Valid: true,
		},
		{
			Name: "valid zero listVersion",
			Message: messages.GetLocalListVersionResponse{
				ListVersion: 0,
			},
			Valid: true,
		},
		{
			Name: "valid negative one listVersion",
			Message: messages.GetLocalListVersionResponse{
				ListVersion: -1,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing listVersion",
			Message: map[string]any{},
			Valid:   false,
		},
		// TODO(parity): needs schema override for minimum:-1 on listVersion.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLocalListVersion16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.GetLocalListVersionRequest, messages.GetLocalListVersionResponse]{
		Action:    v16profiles.GetLocalListVersion.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.GetLocalListVersionRequest) (messages.GetLocalListVersionResponse, error) {
		return messages.GetLocalListVersionResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
