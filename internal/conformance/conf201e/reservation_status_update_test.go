package conf201e

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestReservationStatusUpdate201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "ReservationStatusUpdate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid expired request",
			Message: messages.ReservationStatusUpdateRequest{
				ReservationID:           42,
				ReservationUpdateStatus: "Expired",
			},
			Valid: true,
		},
		{
			Name: "valid removed request",
			Message: messages.ReservationStatusUpdateRequest{
				ReservationID:           42,
				ReservationUpdateStatus: "Removed",
			},
			Valid: true,
		},
		{
			Name: "valid zero reservationId request",
			Message: messages.ReservationStatusUpdateRequest{
				ReservationUpdateStatus: "Expired",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.ReservationStatusUpdateRequest{
				CustomData:              testCustomData201e(),
				ReservationID:           42,
				ReservationUpdateStatus: "Expired",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing reservationId",
			Message: map[string]any{
				"reservationUpdateStatus": "Expired",
			},
			Valid: false,
		},
		{
			Name: "invalid missing reservationUpdateStatus",
			Message: map[string]any{
				"reservationId": 42,
			},
			Valid: false,
		},
		{
			Name: "invalid reservationId below minimum",
			Message: map[string]any{
				"reservationId":           -1,
				"reservationUpdateStatus": "Expired",
			},
			Valid: false,
		},
		{
			Name: "invalid reservationUpdateStatus enum",
			Message: messages.ReservationStatusUpdateRequest{
				ReservationID:           42,
				ReservationUpdateStatus: "invalidReservationStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid customData.vendorId exceeds maxLength 255",
			Message: messages.ReservationStatusUpdateRequest{
				CustomData:              &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				ReservationID:           42,
				ReservationUpdateStatus: "Expired",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReservationStatusUpdate201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "ReservationStatusUpdate", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.ReservationStatusUpdateResponse{},
			Valid:   true,
		},
		{
			Name: "valid full response",
			Message: messages.ReservationStatusUpdateResponse{
				CustomData: testCustomData201e(),
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReservationStatusUpdate201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201e(t, v201profiles.ReservationStatusUpdate)
}
