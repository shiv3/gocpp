package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestChangeAvailability21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.ChangeAvailabilityRequest{
				EVSE: &messages.EVSEType{
					ConnectorID: ptr(int32(1)),
					ID:          1,
				},
				OperationalStatus: "Operative",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing operationalStatus",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid evse missing id",
			Message: map[string]any{
				"evse":              map[string]any{},
				"operationalStatus": "Operative",
			},
			Valid: false,
		},
		{
			Name: "invalid operationalStatus enum",
			Message: messages.ChangeAvailabilityRequest{
				OperationalStatus: "BadEnum",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "ChangeAvailability", "request"), cases)
}

func TestChangeAvailability21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.ChangeAvailabilityResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.ChangeAvailabilityResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo missing reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.ChangeAvailabilityResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.additionalInfo exceeds maxLength 1024",
			Message: messages.ChangeAvailabilityResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: ptr(longString(1025)),
					ReasonCode:     "reason",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "ChangeAvailability", "response"), cases)
}

func TestChangeAvailability21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.ChangeAvailability)
}
