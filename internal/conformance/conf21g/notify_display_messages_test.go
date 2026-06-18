package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func validNotifyDisplayMessagesPayload21() map[string]any {
	return map[string]any{
		"customData": customDataMap21(),
		"messageInfo": []any{
			map[string]any{
				"customData": customDataMap21(),
				"display": map[string]any{
					"customData": customDataMap21(),
					"evse":       map[string]any{"connectorId": 1, "customData": customDataMap21(), "id": 1},
					"instance":   "main",
					"name":       "Display",
				},
				"endDateTime": testTime21().Add(timeHour21).Format(timeFormatRFC3339Nano21),
				"id":          1,
				"message": map[string]any{
					"content":    "Ready",
					"customData": customDataMap21(),
					"format":     "UTF8",
					"language":   "en",
				},
				"messageExtra": []any{
					map[string]any{
						"content":    "Go",
						"customData": customDataMap21(),
						"format":     "ASCII",
						"language":   "en",
					},
				},
				"priority":      "AlwaysFront",
				"startDateTime": testTime21().Format(timeFormatRFC3339Nano21),
				"state":         "Charging",
				"transactionId": "transaction-1",
			},
		},
		"requestId": 1,
		"tbc":       true,
	}
}

func TestNotifyDisplayMessages21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyDisplayMessages", "request")

	validPayload := validNotifyDisplayMessagesPayload21()

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.NotifyDisplayMessagesRequest{
				CustomData:  customData21(),
				MessageInfo: []messages.MessageInfoType{messageInfo21()},
				RequestID:   1,
				Tbc:         boolPtr21(true),
			},
			Valid: true,
		},
		{
			Name:    "missing requestId",
			Message: without21(validPayload, "requestId"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.id",
			Message: without21(validPayload, "messageInfo", 0, "id"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.priority",
			Message: without21(validPayload, "messageInfo", 0, "priority"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.message",
			Message: without21(validPayload, "messageInfo", 0, "message"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.message.format",
			Message: without21(validPayload, "messageInfo", 0, "message", "format"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.message.content",
			Message: without21(validPayload, "messageInfo", 0, "message", "content"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.display.name",
			Message: without21(validPayload, "messageInfo", 0, "display", "name"),
			Valid:   false,
		},
		{
			Name:    "missing messageInfo.display.evse.id",
			Message: without21(validPayload, "messageInfo", 0, "display", "evse", "id"),
			Valid:   false,
		},
		{
			Name:    "invalid messageInfo.priority enum",
			Message: with21(validPayload, "InvalidPriority", "messageInfo", 0, "priority"),
			Valid:   false,
		},
		{
			Name:    "invalid messageInfo.state enum",
			Message: with21(validPayload, "InvalidState", "messageInfo", 0, "state"),
			Valid:   false,
		},
		{
			Name:    "invalid messageInfo.message.format enum",
			Message: with21(validPayload, "Markdown", "messageInfo", 0, "message", "format"),
			Valid:   false,
		},
		{
			Name:    "messageInfo.transactionId exceeds maxLength",
			Message: with21(validPayload, strings.Repeat("x", 37), "messageInfo", 0, "transactionId"),
			Valid:   false,
		},
		{
			Name:    "messageInfo.message.language exceeds maxLength",
			Message: with21(validPayload, strings.Repeat("x", 9), "messageInfo", 0, "message", "language"),
			Valid:   false,
		},
		{
			Name:    "messageInfo.message.content exceeds maxLength",
			Message: with21(validPayload, strings.Repeat("x", 1025), "messageInfo", 0, "message", "content"),
			Valid:   false,
		},
		{
			Name:    "messageInfo.display.name exceeds maxLength",
			Message: with21(validPayload, strings.Repeat("x", 51), "messageInfo", 0, "display", "name"),
			Valid:   false,
		},
		{
			Name:    "messageInfo.display.instance exceeds maxLength",
			Message: with21(validPayload, strings.Repeat("x", 51), "messageInfo", 0, "display", "instance"),
			Valid:   false,
		},
		{
			Name:    "missing customData.vendorId",
			Message: with21(validPayload, map[string]any{}, "customData"),
			Valid:   false,
		},
		{
			Name:    "customData.vendorId exceeds maxLength",
			Message: with21(validPayload, map[string]any{"vendorId": strings.Repeat("x", 256)}, "customData"),
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDisplayMessages21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyDisplayMessages", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.NotifyDisplayMessagesResponse{
				CustomData: customData21(),
			},
			Valid: true,
		},
		{
			Name:    "missing customData.vendorId",
			Message: map[string]any{"customData": map[string]any{}},
			Valid:   false,
		},
		{
			Name:    "customData.vendorId exceeds maxLength",
			Message: map[string]any{"customData": map[string]any{"vendorId": strings.Repeat("x", 256)}},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyDisplayMessages21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifyDisplayMessages)
}
