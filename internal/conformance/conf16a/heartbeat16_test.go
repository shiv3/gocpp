package conf16a

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestHeartbeat16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "Heartbeat", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty request",
			Message: messages.HeartbeatRequest{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestHeartbeat16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "Heartbeat", "response")

	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.HeartbeatResponse{
				CurrentTime: now,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing currentTime",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestHeartbeat16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.HeartbeatRequest, messages.HeartbeatResponse]{
		Action:    v16profiles.Heartbeat.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
