package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestReservationStatusUpdate21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "ReservationStatusUpdate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ReservationStatusUpdateRequest{
				ReservationID:           1,
				ReservationUpdateStatus: "Expired",
			},
			Valid: true,
		},
		{
			Name: "missing reservationUpdateStatus",
			Message: map[string]any{
				"reservationId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.ReservationStatusUpdateRequest{
				CustomData:              invalidCustomData(),
				ReservationID:           1,
				ReservationUpdateStatus: "Expired",
			},
			Valid: false,
		},
		{
			Name: "invalid enum reservationUpdateStatus",
			Message: messages.ReservationStatusUpdateRequest{
				ReservationID:           1,
				ReservationUpdateStatus: "InvalidReservationUpdateStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReservationStatusUpdate21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "ReservationStatusUpdate", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.ReservationStatusUpdateResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.ReservationStatusUpdateResponse{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReservationStatusUpdate21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.ReservationStatusUpdate)
}
