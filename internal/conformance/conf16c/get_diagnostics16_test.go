package conf16c

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestGetDiagnostics16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "GetDiagnostics", "request")

	stopTime := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	startTime := stopTime.Add(-24 * time.Hour)
	retries := int32(10)
	retryInterval := int32(10)
	zeroRetries := int32(0)
	zeroRetryInterval := int32(0)
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.GetDiagnosticsRequest{
				Location:      "ftp:some/path",
				Retries:       &retries,
				RetryInterval: &retryInterval,
				StartTime:     &startTime,
				StopTime:      &stopTime,
			},
			Valid: true,
		},
		{
			Name: "valid zero retries and retryInterval",
			Message: messages.GetDiagnosticsRequest{
				Location:      "ftp:some/path",
				Retries:       &zeroRetries,
				RetryInterval: &zeroRetryInterval,
			},
			Valid: true,
		},
		{
			Name: "valid without stopTime",
			Message: messages.GetDiagnosticsRequest{
				Location:      "ftp:some/path",
				Retries:       &retries,
				RetryInterval: &retryInterval,
				StartTime:     &startTime,
			},
			Valid: true,
		},
		{
			Name: "valid with retries and retryInterval",
			Message: messages.GetDiagnosticsRequest{
				Location:      "ftp:some/path",
				Retries:       &retries,
				RetryInterval: &retryInterval,
			},
			Valid: true,
		},
		{
			Name: "valid with retries",
			Message: messages.GetDiagnosticsRequest{
				Location: "ftp:some/path",
				Retries:  &retries,
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.GetDiagnosticsRequest{
				Location: "ftp:some/path",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing location",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid location format",
			Message: messages.GetDiagnosticsRequest{
				Location: "invalidUri",
			},
			Valid: false,
		},
		{
			Name: "invalid retries below minimum",
			Message: map[string]any{
				"location": "ftp:some/path",
				"retries":  -1,
			},
			Valid: false,
		},
		{
			Name: "invalid retryInterval below minimum",
			Message: map[string]any{
				"location":      "ftp:some/path",
				"retryInterval": -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetDiagnostics16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "GetDiagnostics", "response")

	fileName := "someFileName"
	emptyFileName := ""
	longFileName := strings.Repeat("x", 256)
	cases := []conformance.ValidationCase{
		{
			Name: "valid fileName",
			Message: messages.GetDiagnosticsResponse{
				FileName: &fileName,
			},
			Valid: true,
		},
		{
			Name: "valid empty fileName",
			Message: messages.GetDiagnosticsResponse{
				FileName: &emptyFileName,
			},
			Valid: true,
		},
		{
			Name:    "valid empty response",
			Message: messages.GetDiagnosticsResponse{},
			Valid:   true,
		},
		{
			Name: "invalid fileName exceeds maxLength 255",
			Message: messages.GetDiagnosticsResponse{
				FileName: &longFileName,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetDiagnostics16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.GetDiagnosticsRequest, messages.GetDiagnosticsResponse]{
		Action:    v16profiles.GetDiagnostics.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.GetDiagnosticsRequest) (messages.GetDiagnosticsResponse, error) {
		return messages.GetDiagnosticsResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
