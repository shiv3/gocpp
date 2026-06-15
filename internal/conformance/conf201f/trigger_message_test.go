package conf201f

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestTriggerMessage201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "TriggerMessage", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid with evse",
			Message: messages.TriggerMessageRequest{
				RequestedMessage: "StatusNotification",
				EVSE:             &messages.EVSEType{ID: 1},
			},
			Valid: true,
		},
		{
			Name: "valid without evse",
			Message: messages.TriggerMessageRequest{
				RequestedMessage: "StatusNotification",
			},
			Valid: true,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid requestedMessage enum",
			Message: messages.TriggerMessageRequest{
				RequestedMessage: "invalidMessageTrigger",
				EVSE:             &messages.EVSEType{ID: 1},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid evse.id below minimum")
}

func TestTriggerMessage201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "TriggerMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.TriggerMessageResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo201("200"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.TriggerMessageResponse{
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
			Message: messages.TriggerMessageResponse{
				Status:     "invalidTriggerMessageStatus",
				StatusInfo: statusInfo201("200"),
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
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTriggerMessage201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.TriggerMessage)
}
