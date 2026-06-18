package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestCancelReservation21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "CancelReservation", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.CancelReservationRequest{
				ReservationID: 42,
			},
			Valid: true,
		},
		{
			Name:    "missing reservationId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.CancelReservationRequest{
				CustomData:    &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				ReservationID: 42,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCancelReservation21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "CancelReservation", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.CancelReservationResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.CancelReservationResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.CancelReservationResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCancelReservation21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.CancelReservation)
}
