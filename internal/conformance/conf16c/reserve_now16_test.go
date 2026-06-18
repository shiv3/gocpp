package conf16c

import (
	"context"
	"errors"
	"strings"
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

func TestReserveNow16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "ReserveNow", "request")

	expiryDate := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	parentIDTag := "9999"
	longIDTag := strings.Repeat("x", 21)
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.ReserveNowRequest{
				ConnectorID:   1,
				ExpiryDate:    expiryDate,
				IDTag:         "12345",
				ParentIDTag:   &parentIDTag,
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid without parentIdTag",
			Message: messages.ReserveNowRequest{
				ConnectorID:   1,
				ExpiryDate:    expiryDate,
				IDTag:         "12345",
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid zero connectorId",
			Message: messages.ReserveNowRequest{
				ConnectorID:   0,
				ExpiryDate:    expiryDate,
				IDTag:         "12345",
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid zero reservationId",
			Message: messages.ReserveNowRequest{
				ConnectorID:   1,
				ExpiryDate:    expiryDate,
				IDTag:         "12345",
				ReservationID: 0,
			},
			Valid: true,
		},
		{
			Name: "valid negative reservationId",
			Message: messages.ReserveNowRequest{
				ConnectorID:   1,
				ExpiryDate:    expiryDate,
				IDTag:         "12345",
				ReservationID: -1,
			},
			Valid: true,
		},
		{
			Name: "invalid missing idTag",
			Message: map[string]any{
				"connectorId":   1,
				"expiryDate":    expiryDate,
				"reservationId": 42,
			},
			Valid: false,
		},
		{
			Name: "invalid missing expiryDate",
			Message: map[string]any{
				"connectorId":   1,
				"idTag":         "12345",
				"reservationId": 42,
			},
			Valid: false,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"expiryDate":    expiryDate,
				"idTag":         "12345",
				"reservationId": 42,
			},
			Valid: false,
		},
		{
			Name: "invalid missing reservationId",
			Message: map[string]any{
				"connectorId": 1,
				"expiryDate":  expiryDate,
				"idTag":       "12345",
			},
			Valid: false,
		},
		{
			Name: "invalid idTag exceeds maxLength 20",
			Message: messages.ReserveNowRequest{
				ConnectorID:   1,
				ExpiryDate:    expiryDate,
				IDTag:         longIDTag,
				ReservationID: 42,
			},
			Valid: false,
		},
		{
			Name: "invalid parentIdTag exceeds maxLength 20",
			Message: messages.ReserveNowRequest{
				ConnectorID:   1,
				ExpiryDate:    expiryDate,
				IDTag:         "12345",
				ParentIDTag:   &longIDTag,
				ReservationID: 42,
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId":   -1,
				"expiryDate":    expiryDate,
				"idTag":         "12345",
				"reservationId": 42,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReserveNow16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "ReserveNow", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted status",
			Message: messages.ReserveNowResponse{
				Status: messages.ReserveNowResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.ReserveNowResponse{
				Status: messages.ReserveNowResponseStatus("invalidReserveNowStatus"),
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

func TestReserveNow16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.ReserveNowRequest, messages.ReserveNowResponse]{
		Action:    v16profiles.ReserveNow.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ReserveNowRequest) (messages.ReserveNowResponse, error) {
		return messages.ReserveNowResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
