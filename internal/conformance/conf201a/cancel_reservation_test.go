package conf201a

import (
	"context"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	"github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestCancelReservation201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "CancelReservation", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.CancelReservationRequest{
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.CancelReservationRequest{
				CustomData:    testCustomData(),
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name: "invalid reservationId below minimum",
			Message: map[string]any{
				"reservationId": -1,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing reservationId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid customData.vendorId exceeds maxLength 255",
			Message: messages.CancelReservationRequest{
				CustomData:    &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				ReservationID: 42,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCancelReservation201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "CancelReservation", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal response",
			Message: messages.CancelReservationResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.CancelReservationResponse{
				CustomData: testCustomData(),
				Status:     "Rejected",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.CancelReservationResponse{
				Status: "NotAStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.CancelReservationResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCancelReservation201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.CancelReservationRequest, messages.CancelReservationResponse]{
		Action:    profiles.CancelReservation.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.CancelReservationRequest) (messages.CancelReservationResponse, error) {
		return messages.CancelReservationResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
