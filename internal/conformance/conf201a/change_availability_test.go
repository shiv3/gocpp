package conf201a

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestChangeAvailability201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ChangeAvailability", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid operative with evse and connector",
			Message: messages.ChangeAvailabilityRequest{
				OperationalStatus: "Operative",
				EVSE: &messages.EVSEType{
					ID:          1,
					ConnectorID: int32Ptr(1),
				},
			},
			Valid: true,
		},
		{
			Name: "valid inoperative with evse",
			Message: messages.ChangeAvailabilityRequest{
				OperationalStatus: "Inoperative",
				EVSE:              &messages.EVSEType{ID: 1},
			},
			Valid: true,
		},
		{
			Name: "valid inoperative without evse",
			Message: messages.ChangeAvailabilityRequest{
				OperationalStatus: "Inoperative",
			},
			Valid: true,
		},
		{
			Name: "valid operative without evse",
			Message: messages.ChangeAvailabilityRequest{
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
			Name: "invalid operationalStatus enum",
			Message: messages.ChangeAvailabilityRequest{
				OperationalStatus: "BadEnum",
			},
			Valid: false,
		},
		{
			Name: "invalid evse missing id",
			Message: map[string]any{
				"operationalStatus": "Operative",
				"evse":              map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid evse id below minimum",
			Message: map[string]any{
				"operationalStatus": "Operative",
				"evse":              map[string]any{"id": -1},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestChangeAvailability201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ChangeAvailability", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.ChangeAvailabilityResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.ChangeAvailabilityResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid scheduled response",
			Message: messages.ChangeAvailabilityResponse{
				Status: "Scheduled",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.ChangeAvailabilityResponse{
				CustomData: testCustomData(),
				Status:     "Accepted",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.ChangeAvailabilityResponse{
				Status: "BadEnum",
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

func TestChangeAvailability201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.ChangeAvailabilityRequest, messages.ChangeAvailabilityResponse]{
		Action:    v201profiles.ChangeAvailability.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.ChangeAvailabilityRequest) (messages.ChangeAvailabilityResponse, error) {
		return messages.ChangeAvailabilityResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
