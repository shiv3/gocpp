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

func transactionInfo201() messages.TransactionType {
	return messages.TransactionType{
		TransactionID:     "42",
		ChargingState:     ptr("SuspendedEV"),
		TimeSpentCharging: ptr(int32(100)),
		StoppedReason:     ptr("Local"),
		RemoteStartID:     ptr(int32(7)),
	}
}

func meterValue201() messages.MeterValueType {
	return messages.MeterValueType{
		Timestamp: fixedTime201(),
		SampledValue: []messages.SampledValueType{
			{Value: dec("64.0")},
		},
	}
}

func TestTransactionEvent201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "TransactionEvent", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
				IDToken:            ptr(idToken201("KeyCode")),
				EVSE:               &messages.EVSEType{ID: 1},
				MeterValue:         []messages.MeterValueType{meterValue201()},
			},
			Valid: true,
		},
		{
			Name: "valid empty meterValue omitted",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
				IDToken:            ptr(idToken201("KeyCode")),
				EVSE:               &messages.EVSEType{ID: 1},
				MeterValue:         []messages.MeterValueType{},
			},
			Valid: true,
		},
		{
			Name: "valid without meterValue",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
				IDToken:            ptr(idToken201("KeyCode")),
				EVSE:               &messages.EVSEType{ID: 1},
			},
			Valid: true,
		},
		{
			Name: "valid without evse",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
				IDToken:            ptr(idToken201("KeyCode")),
			},
			Valid: true,
		},
		{
			Name: "valid without idToken",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
			},
			Valid: true,
		},
		{
			Name: "valid without reservationId",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				TransactionInfo:    transactionInfo201(),
			},
			Valid: true,
		},
		{
			Name: "valid without cableMaxCurrent",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				TransactionInfo:    transactionInfo201(),
			},
			Valid: true,
		},
		{
			Name: "valid without numberOfPhasesUsed",
			Message: messages.TransactionEventRequest{
				EventType:       "Started",
				Timestamp:       fixedTime201(),
				TriggerReason:   "Authorized",
				SeqNo:           1,
				Offline:         ptr(true),
				TransactionInfo: transactionInfo201(),
			},
			Valid: true,
		},
		{
			Name: "valid without offline",
			Message: messages.TransactionEventRequest{
				EventType:       "Started",
				Timestamp:       fixedTime201(),
				TriggerReason:   "Authorized",
				SeqNo:           1,
				TransactionInfo: transactionInfo201(),
			},
			Valid: true,
		},
		{
			Name: "valid zero seqNo",
			Message: messages.TransactionEventRequest{
				EventType:       "Started",
				Timestamp:       fixedTime201(),
				TriggerReason:   "Authorized",
				TransactionInfo: transactionInfo201(),
			},
			Valid: true,
		},
		{
			Name: "valid no authorization idToken",
			Message: messages.TransactionEventRequest{
				EventType:       "Started",
				Timestamp:       fixedTime201(),
				TriggerReason:   "Authorized",
				TransactionInfo: transactionInfo201(),
				IDToken:         &messages.IdTokenType{Type: "NoAuthorization"},
			},
			Valid: true,
		},
		{
			Name: "invalid missing transactionInfo",
			Message: map[string]any{
				"eventType":     "Started",
				"timestamp":     fixedTime201(),
				"triggerReason": "Authorized",
			},
			Valid: false,
		},
		{
			Name: "invalid missing triggerReason",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201(),
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"eventType":       "Started",
				"triggerReason":   "Authorized",
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name: "invalid missing eventType",
			Message: map[string]any{
				"timestamp":       fixedTime201(),
				"triggerReason":   "Authorized",
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid eventType enum",
			Message: messages.TransactionEventRequest{
				EventType:          "invalidEventType",
				Timestamp:          fixedTime201(),
				TriggerReason:      "Authorized",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
				IDToken:            ptr(idToken201("KeyCode")),
				EVSE:               &messages.EVSEType{ID: 1},
				MeterValue:         []messages.MeterValueType{meterValue201()},
			},
			Valid: false,
		},
		{
			Name: "invalid triggerReason enum",
			Message: messages.TransactionEventRequest{
				EventType:          "Started",
				Timestamp:          fixedTime201(),
				TriggerReason:      "invalidTriggerReason",
				SeqNo:              1,
				Offline:            ptr(true),
				NumberOfPhasesUsed: ptr(int32(3)),
				CableMaxCurrent:    ptr(int32(20)),
				ReservationID:      ptr(int32(42)),
				TransactionInfo:    transactionInfo201(),
				IDToken:            ptr(idToken201("KeyCode")),
				EVSE:               &messages.EVSEType{ID: 1},
				MeterValue:         []messages.MeterValueType{meterValue201()},
			},
			Valid: false,
		},
		{
			Name: "invalid missing transactionId",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid empty idToken",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
				"idToken":         map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid empty meterValue",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
				"meterValue":      []any{map[string]any{}},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid keyCode idToken without token value")
	skipSchemaOverride201(t, "invalid seqNo below minimum")
	skipSchemaOverride201(t, "invalid numberOfPhasesUsed below minimum")
	skipSchemaOverride201(t, "invalid evse.id below minimum")
}

func TestTransactionEvent201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "TransactionEvent", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.TransactionEventResponse{
				TotalCost:              ptr(dec("8.42")),
				ChargingPriority:       ptr(int32(2)),
				IDTokenInfo:            &messages.IdTokenInfoType{Status: "Accepted"},
				UpdatedPersonalMessage: ptr(messageContent201()),
			},
			Valid: true,
		},
		{
			Name: "valid without updatedPersonalMessage",
			Message: messages.TransactionEventResponse{
				TotalCost:        ptr(dec("8.42")),
				ChargingPriority: ptr(int32(2)),
				IDTokenInfo:      &messages.IdTokenInfoType{Status: "Accepted"},
			},
			Valid: true,
		},
		{
			Name: "valid without idTokenInfo",
			Message: messages.TransactionEventResponse{
				TotalCost:        ptr(dec("8.42")),
				ChargingPriority: ptr(int32(2)),
			},
			Valid: true,
		},
		{
			Name: "valid without chargingPriority",
			Message: messages.TransactionEventResponse{
				TotalCost: ptr(dec("8.42")),
			},
			Valid: true,
		},
		{
			Name:    "valid empty response",
			Message: messages.TransactionEventResponse{},
			Valid:   true,
		},
		{
			Name: "invalid idTokenInfo status enum",
			Message: messages.TransactionEventResponse{
				TotalCost:              ptr(dec("8.42")),
				ChargingPriority:       ptr(int32(2)),
				IDTokenInfo:            &messages.IdTokenInfoType{Status: "invalidAuthorizationStatus"},
				UpdatedPersonalMessage: ptr(messageContent201()),
			},
			Valid: false,
		},
		{
			Name: "invalid empty updatedPersonalMessage",
			Message: messages.TransactionEventResponse{
				TotalCost:              ptr(dec("8.42")),
				ChargingPriority:       ptr(int32(2)),
				IDTokenInfo:            &messages.IdTokenInfoType{Status: "Accepted"},
				UpdatedPersonalMessage: &messages.MessageContentType{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid totalCost below minimum")
	skipSchemaOverride201(t, "invalid chargingPriority below minimum")
	skipSchemaOverride201(t, "invalid chargingPriority above maximum")
}

func TestTransactionEvent201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.TransactionEvent)
}
