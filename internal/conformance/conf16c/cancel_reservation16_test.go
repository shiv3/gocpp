package conf16c

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestCancelReservation16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "CancelReservation", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid reservationId",
			Message: messages.CancelReservationRequest{
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid zero reservationId",
			Message: messages.CancelReservationRequest{
				ReservationID: 0,
			},
			Valid: true,
		},
		{
			Name: "valid negative reservationId",
			Message: messages.CancelReservationRequest{
				ReservationID: -1,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing reservationId",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCancelReservation16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "CancelReservation", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted status",
			Message: messages.CancelReservationResponse{
				Status: messages.CancelReservationResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.CancelReservationResponse{
				Status: messages.CancelReservationResponseStatus("invalidCancelReservationStatus"),
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

func TestCancelReservation16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.CancelReservationRequest, messages.CancelReservationResponse]{
		Action:    v16profiles.CancelReservation.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.CancelReservationRequest) (messages.CancelReservationResponse, error) {
		return messages.CancelReservationResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
