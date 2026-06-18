package conf16a

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestChangeAvailability16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "ChangeAvailability", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid operative request",
			Message: messages.ChangeAvailabilityRequest{
				ConnectorID: 0,
				Type:        messages.ChangeAvailabilityRequestTypeOperative,
			},
			Valid: true,
		},
		{
			Name: "valid inoperative request",
			Message: messages.ChangeAvailabilityRequest{
				ConnectorID: 0,
				Type:        messages.ChangeAvailabilityRequestTypeInoperative,
			},
			Valid: true,
		},
		{
			Name: "invalid missing type",
			Message: map[string]any{
				"connectorId": 0,
			},
			Valid: false,
		},
		{
			Name: "valid default connectorId with operative type",
			Message: messages.ChangeAvailabilityRequest{
				Type: messages.ChangeAvailabilityRequestTypeOperative,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown type enum",
			Message: messages.ChangeAvailabilityRequest{
				ConnectorID: 0,
				Type:        messages.ChangeAvailabilityRequestType("invalidAvailabilityType"),
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId": -1,
				"type":        messages.ChangeAvailabilityRequestTypeOperative,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestChangeAvailability16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "ChangeAvailability", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.ChangeAvailabilityResponse{
				Status: messages.ChangeAvailabilityResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.ChangeAvailabilityResponse{
				Status: messages.ChangeAvailabilityResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "valid scheduled response",
			Message: messages.ChangeAvailabilityResponse{
				Status: messages.ChangeAvailabilityResponseStatusScheduled,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.ChangeAvailabilityResponse{
				Status: messages.ChangeAvailabilityResponseStatus("invalidAvailabilityStatus"),
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

func TestChangeAvailability16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.ChangeAvailabilityRequest, messages.ChangeAvailabilityResponse]{
		Action:    v16profiles.ChangeAvailability.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ChangeAvailabilityRequest) (messages.ChangeAvailabilityResponse, error) {
		return messages.ChangeAvailabilityResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
