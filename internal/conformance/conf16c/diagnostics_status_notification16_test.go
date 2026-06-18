package conf16c

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestDiagnosticsStatusNotification16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "DiagnosticsStatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid uploaded status",
			Message: messages.DiagnosticsStatusNotificationRequest{
				Status: messages.DiagnosticsStatusNotificationRequestStatusUploaded,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.DiagnosticsStatusNotificationRequest{
				Status: messages.DiagnosticsStatusNotificationRequestStatus("invalidDiagnosticsStatus"),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDiagnosticsStatusNotification16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "DiagnosticsStatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.DiagnosticsStatusNotificationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDiagnosticsStatusNotification16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.DiagnosticsStatusNotificationRequest, messages.DiagnosticsStatusNotificationResponse]{
		Action:    v16profiles.DiagnosticsStatusNotification.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.DiagnosticsStatusNotificationRequest) (messages.DiagnosticsStatusNotificationResponse, error) {
		return messages.DiagnosticsStatusNotificationResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
