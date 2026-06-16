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
			Name:    "invalid missing requestedMessage",
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
		{
			Name: "invalid missing evse.id",
			Message: map[string]any{
				"requestedMessage": "StatusNotification",
				"evse":             map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid evse id below minimum",
			Message: map[string]any{
				"requestedMessage": "StatusNotification",
				"evse":             map[string]any{"id": -1},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
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
				StatusInfo: testStatusInfo201f(),
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
				StatusInfo: testStatusInfo201f(),
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
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTriggerMessage201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.TriggerMessage)
}
