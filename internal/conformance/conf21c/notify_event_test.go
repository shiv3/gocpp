package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestNotifyEvent21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyEvent", "request")

	validEventData := messages.EventDataType{
		ActualValue:           "value",
		Component:             component21(),
		EventID:               1,
		EventNotificationType: "HardWiredNotification",
		Timestamp:             fixedTime21(),
		Trigger:               "Alerting",
		Variable:              variable21(),
	}

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyEventRequest{
				EventData:   []messages.EventDataType{validEventData},
				GeneratedAt: fixedTime21(),
				SeqNo:       1,
			},
			Valid: true,
		},
		{
			Name: "missing eventData",
			Message: map[string]any{
				"generatedAt": fixedTime21(),
				"seqNo":       1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.NotifyEventRequest{
				EventData: []messages.EventDataType{
					{
						ActualValue:           longString(2501),
						Component:             component21(),
						EventID:               1,
						EventNotificationType: "HardWiredNotification",
						Timestamp:             fixedTime21(),
						Trigger:               "Alerting",
						Variable:              variable21(),
					},
				},
				GeneratedAt: fixedTime21(),
				SeqNo:       1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.NotifyEventRequest{
				EventData: []messages.EventDataType{
					{
						ActualValue:           "value",
						Component:             component21(),
						EventID:               1,
						EventNotificationType: "BogusNotification",
						Timestamp:             fixedTime21(),
						Trigger:               "Alerting",
						Variable:              variable21(),
					},
				},
				GeneratedAt: fixedTime21(),
				SeqNo:       1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEvent21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyEvent", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyEventResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.NotifyEventResponse{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEvent21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifyEvent)
}
