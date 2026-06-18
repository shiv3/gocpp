package conf16c

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestUpdateFirmware16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "UpdateFirmware", "request")

	retrieveDate := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	retries := int32(10)
	retryInterval := int32(10)
	zeroRetries := int32(0)
	zeroRetryInterval := int32(0)
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.UpdateFirmwareRequest{
				Location:      "ftp:some/path",
				Retries:       &retries,
				RetrieveDate:  retrieveDate,
				RetryInterval: &retryInterval,
			},
			Valid: true,
		},
		{
			Name: "valid zero retries and retryInterval",
			Message: messages.UpdateFirmwareRequest{
				Location:      "ftp:some/path",
				Retries:       &zeroRetries,
				RetrieveDate:  retrieveDate,
				RetryInterval: &zeroRetryInterval,
			},
			Valid: true,
		},
		{
			Name: "valid without retryInterval",
			Message: messages.UpdateFirmwareRequest{
				Location:     "ftp:some/path",
				Retries:      &retries,
				RetrieveDate: retrieveDate,
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.UpdateFirmwareRequest{
				Location:     "ftp:some/path",
				RetrieveDate: retrieveDate,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing location and retrieveDate",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing retrieveDate",
			Message: map[string]any{
				"location": "ftp:some/path",
			},
			Valid: false,
		},
		{
			Name: "invalid location format",
			Message: messages.UpdateFirmwareRequest{
				Location:     "invalidUri",
				RetrieveDate: retrieveDate,
			},
			Valid: false,
		},
		{
			Name: "invalid retries below minimum",
			Message: map[string]any{
				"location":     "ftp:some/path",
				"retries":      -1,
				"retrieveDate": retrieveDate,
			},
			Valid: false,
		},
		{
			Name: "invalid retryInterval below minimum",
			Message: map[string]any{
				"location":      "ftp:some/path",
				"retrieveDate":  retrieveDate,
				"retryInterval": -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateFirmware16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "UpdateFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.UpdateFirmwareResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUpdateFirmware16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.UpdateFirmwareRequest, messages.UpdateFirmwareResponse]{
		Action:    v16profiles.UpdateFirmware.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.UpdateFirmwareRequest) (messages.UpdateFirmwareResponse, error) {
		return messages.UpdateFirmwareResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
