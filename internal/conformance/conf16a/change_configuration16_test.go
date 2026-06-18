package conf16a

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestChangeConfiguration16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "ChangeConfiguration", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.ChangeConfigurationRequest{
				Key:   "someKey",
				Value: "someValue",
			},
			Valid: true,
		},
		{
			Name: "invalid missing value",
			Message: map[string]any{
				"key": "someKey",
			},
			Valid: false,
		},
		{
			Name: "invalid missing key",
			Message: map[string]any{
				"value": "someValue",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing key and value",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid key exceeds maxLength 50",
			Message: messages.ChangeConfigurationRequest{
				Key:   strings.Repeat("x", 51),
				Value: "someValue",
			},
			Valid: false,
		},
		{
			Name: "invalid value exceeds maxLength 500",
			Message: messages.ChangeConfigurationRequest{
				Key:   "someKey",
				Value: strings.Repeat("x", 501),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestChangeConfiguration16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "ChangeConfiguration", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.ChangeConfigurationResponse{
				Status: messages.ChangeConfigurationResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.ChangeConfigurationResponse{
				Status: messages.ChangeConfigurationResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "valid reboot required response",
			Message: messages.ChangeConfigurationResponse{
				Status: messages.ChangeConfigurationResponseStatusRebootRequired,
			},
			Valid: true,
		},
		{
			Name: "valid not supported response",
			Message: messages.ChangeConfigurationResponse{
				Status: messages.ChangeConfigurationResponseStatusNotSupported,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.ChangeConfigurationResponse{
				Status: messages.ChangeConfigurationResponseStatus("invalidConfigurationStatus"),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestChangeConfiguration16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.ChangeConfigurationRequest, messages.ChangeConfigurationResponse]{
		Action:    v16profiles.ChangeConfiguration.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ChangeConfigurationRequest) (messages.ChangeConfigurationResponse, error) {
		return messages.ChangeConfigurationResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
