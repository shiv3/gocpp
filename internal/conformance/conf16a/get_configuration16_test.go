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

func TestGetConfiguration16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "GetConfiguration", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid two keys request",
			Message: messages.GetConfigurationRequest{
				Key: []string{"key1", "key2"},
			},
			Valid: true,
		},
		{
			Name: "valid six keys request",
			Message: messages.GetConfigurationRequest{
				Key: []string{"key1", "key2", "key3", "key4", "key5", "key6"},
			},
			Valid: true,
		},
		{
			Name: "invalid duplicate key values",
			Message: map[string]any{
				"key": []string{"key1", "key1"},
			},
			Valid: false,
		},
		{
			Name:    "valid empty request",
			Message: messages.GetConfigurationRequest{},
			Valid:   true,
		},
		{
			Name: "valid empty key array",
			Message: map[string]any{
				"key": []string{},
			},
			Valid: true,
		},
		{
			Name: "invalid key exceeds maxLength 50",
			Message: messages.GetConfigurationRequest{
				Key: []string{strings.Repeat("x", 51)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetConfiguration16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "GetConfiguration", "response")

	value1 := "value1"
	value2 := "value2"
	cases := []conformance.ValidationCase{
		{
			Name: "valid one configurationKey response",
			Message: messages.GetConfigurationResponse{
				ConfigurationKey: []messages.ConfigurationKey{
					{Key: "key1", Readonly: true, Value: &value1},
				},
			},
			Valid: true,
		},
		{
			Name: "valid two configurationKeys response",
			Message: messages.GetConfigurationResponse{
				ConfigurationKey: []messages.ConfigurationKey{
					{Key: "key1", Readonly: true, Value: &value1},
					{Key: "key2", Readonly: false, Value: &value2},
				},
			},
			Valid: true,
		},
		{
			Name: "valid configurationKey and unknownKey response",
			Message: messages.GetConfigurationResponse{
				ConfigurationKey: []messages.ConfigurationKey{
					{Key: "key1", Readonly: true, Value: &value1},
				},
				UnknownKey: []string{"keyX"},
			},
			Valid: true,
		},
		{
			Name: "valid readonly false and multiple unknownKeys response",
			Message: messages.GetConfigurationResponse{
				ConfigurationKey: []messages.ConfigurationKey{
					{Key: "key1", Readonly: false, Value: &value1},
				},
				UnknownKey: []string{"keyX", "keyY"},
			},
			Valid: true,
		},
		{
			Name: "valid unknownKey only response",
			Message: messages.GetConfigurationResponse{
				UnknownKey: []string{"keyX"},
			},
			Valid: true,
		},
		{
			Name: "invalid unknownKey exceeds maxLength 50",
			Message: messages.GetConfigurationResponse{
				UnknownKey: []string{strings.Repeat("x", 51)},
			},
			Valid: false,
		},
		{
			Name: "invalid configurationKey key exceeds maxLength 50",
			Message: messages.GetConfigurationResponse{
				ConfigurationKey: []messages.ConfigurationKey{
					{Key: strings.Repeat("x", 51), Readonly: true, Value: &value1},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid configurationKey value exceeds maxLength 500",
			Message: messages.GetConfigurationResponse{
				ConfigurationKey: []messages.ConfigurationKey{
					{Key: "key1", Readonly: true, Value: strPtr(strings.Repeat("x", 501))},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetConfiguration16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.GetConfigurationRequest, messages.GetConfigurationResponse]{
		Action:    v16profiles.GetConfiguration.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.GetConfigurationRequest) (messages.GetConfigurationResponse, error) {
		return messages.GetConfigurationResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
