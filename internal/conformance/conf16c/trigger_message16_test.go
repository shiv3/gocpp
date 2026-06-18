package conf16c

import (
	"context"
	"errors"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	messages "github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestTriggerMessage16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "TriggerMessage", "request")

	connectorID := int32(1)
	zeroConnectorID := int32(0)
	cases := []conformance.ValidationCase{
		{
			Name: "valid requestedMessage with connectorId",
			Message: messages.TriggerMessageRequest{
				RequestedMessage: messages.TriggerMessageRequestRequestedMessageStatusNotification,
				ConnectorID:      &connectorID,
			},
			Valid: true,
		},
		{
			Name: "valid requestedMessage without connectorId",
			Message: messages.TriggerMessageRequest{
				RequestedMessage: messages.TriggerMessageRequestRequestedMessageStatusNotification,
			},
			Valid: true,
		},
		{
			Name: "valid zero connectorId",
			Message: messages.TriggerMessageRequest{
				RequestedMessage: messages.TriggerMessageRequestRequestedMessageStatusNotification,
				ConnectorID:      &zeroConnectorID,
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
				RequestedMessage: messages.TriggerMessageRequestRequestedMessage("StartTransaction"),
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"requestedMessage": messages.TriggerMessageRequestRequestedMessageStatusNotification,
				"connectorId":      -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTriggerMessage16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "TriggerMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted status",
			Message: messages.TriggerMessageResponse{
				Status: messages.TriggerMessageResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.TriggerMessageResponse{
				Status: messages.TriggerMessageResponseStatus("invalidTriggerMessageStatus"),
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

func TestTriggerMessage16_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid")
	wrongDirection := ocppj.Message[messages.TriggerMessageRequest, messages.TriggerMessageResponse]{
		Action:    v16profiles.TriggerMessage.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.TriggerMessageRequest) (messages.TriggerMessageResponse, error) {
		return messages.TriggerMessageResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
