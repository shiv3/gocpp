package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestNotifyDisplayMessages201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyDisplayMessages", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.NotifyDisplayMessagesRequest{
				RequestID:   42,
				Tbc:         ptr(false),
				MessageInfo: []messages.MessageInfoType{messageInfo201()},
			},
			Valid: true,
		},
		{
			Name: "valid empty messageInfo omitted",
			Message: messages.NotifyDisplayMessagesRequest{
				RequestID:   42,
				Tbc:         ptr(false),
				MessageInfo: []messages.MessageInfoType{},
			},
			Valid: true,
		},
		{
			Name: "valid without messageInfo",
			Message: messages.NotifyDisplayMessagesRequest{
				RequestID: 42,
				Tbc:       ptr(false),
			},
			Valid: true,
		},
		{
			Name: "valid without tbc",
			Message: messages.NotifyDisplayMessagesRequest{
				RequestID: 42,
			},
			Valid: true,
		},
		{
			Name:    "valid zero requestId",
			Message: messages.NotifyDisplayMessagesRequest{},
			Valid:   true,
		},
		{
			Name: "invalid messageInfo priority enum",
			Message: messages.NotifyDisplayMessagesRequest{
				RequestID: 42,
				MessageInfo: []messages.MessageInfoType{
					{
						ID:       42,
						Priority: "invalidPriority",
						State:    ptr("Idle"),
						Message:  messageContent201(),
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid requestId below minimum")
}

func TestNotifyDisplayMessages201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyDisplayMessages", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.NotifyDisplayMessagesResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDisplayMessages201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyDisplayMessages)
}
