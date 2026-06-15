package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func eventData201() messages.EventDataType {
	return messages.EventDataType{
		EventID:               1,
		Timestamp:             fixedTime201(),
		Trigger:               "Alerting",
		Cause:                 ptr(int32(42)),
		ActualValue:           "someValue",
		TechCode:              ptr("742"),
		TechInfo:              ptr("stacktrace"),
		Cleared:               ptr(false),
		TransactionID:         ptr("1234"),
		VariableMonitoringID:  ptr(int32(99)),
		EventNotificationType: "PreconfiguredMonitor",
		Component:             component201(),
		Variable:              variable201(),
	}
}

func eventDataMap201() map[string]any {
	return map[string]any{
		"eventId":               1,
		"timestamp":             fixedTime201(),
		"trigger":               "Alerting",
		"cause":                 42,
		"actualValue":           "someValue",
		"techCode":              "742",
		"techInfo":              "stacktrace",
		"cleared":               false,
		"transactionId":         "1234",
		"variableMonitoringId":  99,
		"eventNotificationType": "PreconfiguredMonitor",
		"component":             map[string]any{"name": "component1"},
		"variable":              map[string]any{"name": "variable1"},
	}
}

func TestNotifyEvent201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyEvent", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid two events",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				SeqNo:       0,
				Tbc:         ptr(false),
				EventData:   []messages.EventDataType{eventData201(), eventData201()},
			},
			Valid: true,
		},
		{
			Name: "valid one event",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				SeqNo:       0,
				Tbc:         ptr(false),
				EventData:   []messages.EventDataType{eventData201()},
			},
			Valid: true,
		},
		{
			Name: "valid without tbc",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				SeqNo:       0,
				EventData:   []messages.EventDataType{eventData201()},
			},
			Valid: true,
		},
		{
			Name: "valid zero seqNo",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData:   []messages.EventDataType{eventData201()},
			},
			Valid: true,
		},
		{
			Name: "valid minimal event data",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					{
						Timestamp:             fixedTime201(),
						Trigger:               "Alerting",
						ActualValue:           "someValue",
						EventNotificationType: "PreconfiguredMonitor",
						Component:             messages.ComponentType{Name: "component1"},
						Variable:              messages.VariableType{Name: "variable1"},
					},
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing generatedAt",
			Message: map[string]any{
				"seqNo":     0,
				"tbc":       false,
				"eventData": []any{eventDataMap201()},
			},
			Valid: false,
		},
		{
			Name: "invalid empty request",
			Message: map[string]any{
				"eventData": []any{eventDataMap201()},
			},
			Valid: false,
		},
		{
			Name: "invalid empty eventData",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"seqNo":       0,
				"tbc":         false,
				"eventData":   []any{},
			},
			Valid: false,
		},
		{
			Name: "invalid missing eventData",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"seqNo":       0,
				"tbc":         false,
			},
			Valid: false,
		},
		{
			Name: "invalid event missing eventNotificationType",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"seqNo":       0,
				"tbc":         false,
				"eventData": []any{
					map[string]any{
						"timestamp":   fixedTime201(),
						"trigger":     "Alerting",
						"cause":       42,
						"actualValue": "someValue",
						"component":   map[string]any{"name": "component1"},
						"variable":    map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid event missing timestamp",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"eventData": []any{
					map[string]any{
						"trigger":               "Alerting",
						"actualValue":           "someValue",
						"eventNotificationType": "PreconfiguredMonitor",
						"component":             map[string]any{"name": "component1"},
						"variable":              map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid event missing trigger",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"eventData": []any{
					map[string]any{
						"timestamp":             fixedTime201(),
						"actualValue":           "someValue",
						"eventNotificationType": "PreconfiguredMonitor",
						"component":             map[string]any{"name": "component1"},
						"variable":              map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid event missing actualValue",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"eventData": []any{
					map[string]any{
						"timestamp":             fixedTime201(),
						"trigger":               "Alerting",
						"eventNotificationType": "PreconfiguredMonitor",
						"component":             map[string]any{"name": "component1"},
						"variable":              map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid event missing variable",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"eventData": []any{
					map[string]any{
						"timestamp":             fixedTime201(),
						"trigger":               "Alerting",
						"actualValue":           "someValue",
						"eventNotificationType": "PreconfiguredMonitor",
						"component":             map[string]any{"name": "component1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid event missing component",
			Message: map[string]any{
				"generatedAt": fixedTime201(),
				"eventData": []any{
					map[string]any{
						"timestamp":             fixedTime201(),
						"trigger":               "Alerting",
						"actualValue":           "someValue",
						"eventNotificationType": "PreconfiguredMonitor",
						"variable":              map[string]any{"name": "variable1"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid trigger enum",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.Trigger = "invalidEventTrigger"
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid actualValue exceeds maxLength 2500",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.ActualValue = longString(2501)
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid techCode exceeds maxLength 50",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.TechCode = ptr(longString(51))
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid techInfo exceeds maxLength 500",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.TechInfo = ptr(longString(501))
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid transactionId exceeds maxLength 36",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.TransactionID = ptr(longString(37))
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid eventNotificationType enum",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.EventNotificationType = "invalidEventNotification"
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid component name exceeds maxLength 50",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.Component = messages.ComponentType{Name: longString(51)}
						return data
					}(),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid variable name exceeds maxLength 50",
			Message: messages.NotifyEventRequest{
				GeneratedAt: fixedTime201(),
				EventData: []messages.EventDataType{
					func() messages.EventDataType {
						data := eventData201()
						data.Variable = messages.VariableType{Name: longString(51)}
						return data
					}(),
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid seqNo below minimum")
	skipSchemaOverride201(t, "invalid eventId below minimum")
}

func TestNotifyEvent201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyEvent", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.NotifyEventResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEvent201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyEvent)
}
