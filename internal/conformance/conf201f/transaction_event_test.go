package conf201f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func testTransactionInfo201f() messages.TransactionType {
	return messages.TransactionType{
		TransactionID:     "42",
		ChargingState:     strPtr201f("SuspendedEV"),
		TimeSpentCharging: int32Ptr201f(100),
		StoppedReason:     strPtr201f("Local"),
		RemoteStartID:     int32Ptr201f(7),
	}
}

func testMeterValue201f() messages.MeterValueType {
	return messages.MeterValueType{
		Timestamp: fixedTime201f(),
		SampledValue: []messages.SampledValueType{
			{
				Value: decimal201f("64"),
			},
		},
	}
}

func testTransactionEventRequest201f() messages.TransactionEventRequest {
	return messages.TransactionEventRequest{
		EventType:          "Started",
		Timestamp:          fixedTime201f(),
		TriggerReason:      "Authorized",
		SeqNo:              1,
		Offline:            boolPtr201f(true),
		NumberOfPhasesUsed: int32Ptr201f(3),
		CableMaxCurrent:    int32Ptr201f(20),
		ReservationID:      int32Ptr201f(42),
		TransactionInfo:    testTransactionInfo201f(),
		IDToken:            &messages.IdTokenType{IDToken: "1234", Type: "KeyCode"},
		EVSE:               &messages.EVSEType{ID: 1},
		MeterValue:         []messages.MeterValueType{testMeterValue201f()},
	}
}

func TestTransactionEvent201_RequestValidation(t *testing.T) {
	useDecimalJSONWithoutQuotes201f(t)

	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "TransactionEvent", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid full request",
			Message: testTransactionEventRequest201f(),
			Valid:   true,
		},
		{
			Name: "valid transactionInfo without remoteStartId",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.RemoteStartID = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid transactionInfo without stoppedReason",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.StoppedReason = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid transactionInfo without timeSpentCharging",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.TimeSpentCharging = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid transactionInfo without chargingState",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.ChargingState = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid minimal transactionInfo",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo = messages.TransactionType{TransactionID: "42"}
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without meterValue",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.MeterValue = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without evse",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.EVSE = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without idToken",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.IDToken = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without reservationId",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.ReservationID = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without cableMaxCurrent",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.CableMaxCurrent = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without numberOfPhasesUsed",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.NumberOfPhasesUsed = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid without offline",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.Offline = nil
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid zero seqNo",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.SeqNo = 0
				return req
			}(),
			Valid: true,
		},
		{
			Name: "valid no-authorization idToken without token value",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.IDToken = &messages.IdTokenType{Type: "NoAuthorization"}
				return req
			}(),
			Valid: true,
		},
		{
			Name: "invalid missing idToken.idToken",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201f(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
				"idToken":         map[string]any{"type": "KeyCode"},
			},
			Valid: false,
		},
		{
			Name: "invalid missing transactionInfo",
			Message: map[string]any{
				"eventType":     "Started",
				"timestamp":     fixedTime201f(),
				"triggerReason": "Authorized",
				"seqNo":         1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing triggerReason",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201f(),
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"eventType":       "Started",
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name: "invalid missing eventType",
			Message: map[string]any{
				"timestamp":       fixedTime201f(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name: "invalid missing seqNo",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201f(),
				"triggerReason":   "Authorized",
				"transactionInfo": map[string]any{"transactionId": "42"},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid eventType enum",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.EventType = "invalidEventType"
				return req
			}(),
			Valid: false,
		},
		{
			Name: "invalid triggerReason enum",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TriggerReason = "invalidTriggerReason"
				return req
			}(),
			Valid: false,
		},
		{
			Name: "invalid transactionInfo missing transactionId",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201f(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid transactionId exceeds maxLength 36",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.TransactionID = strings.Repeat("x", 37)
				return req
			}(),
			Valid: false,
		},
		{
			Name: "invalid chargingState enum",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.ChargingState = strPtr201f("invalidChargingState")
				return req
			}(),
			Valid: false,
		},
		{
			Name: "invalid stoppedReason enum",
			Message: func() messages.TransactionEventRequest {
				req := testTransactionEventRequest201f()
				req.TransactionInfo.StoppedReason = strPtr201f("invalidReason")
				return req
			}(),
			Valid: false,
		},
		{
			Name: "invalid missing idToken fields",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201f(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
				"idToken":         map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid meterValue missing required fields",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime201f(),
				"triggerReason":   "Authorized",
				"seqNo":           1,
				"transactionInfo": map[string]any{"transactionId": "42"},
				"meterValue":      []any{map[string]any{}},
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for empty optional meterValue array.
		// TODO(parity): needs schema override for seqNo minimum.
		// TODO(parity): needs schema override for numberOfPhasesUsed minimum.
		// TODO(parity): needs schema override for evse.id minimum.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTransactionEvent201_ResponseValidation(t *testing.T) {
	useDecimalJSONWithoutQuotes201f(t)

	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "TransactionEvent", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.TransactionEventResponse{
				TotalCost:              decimalPtr201f("8.42"),
				ChargingPriority:       int32Ptr201f(2),
				IDTokenInfo:            &messages.IdTokenInfoType{Status: "Accepted"},
				UpdatedPersonalMessage: &messages.MessageContentType{Format: "UTF8", Content: "dummyContent"},
			},
			Valid: true,
		},
		{
			Name: "valid without updatedPersonalMessage",
			Message: messages.TransactionEventResponse{
				TotalCost:        decimalPtr201f("8.42"),
				ChargingPriority: int32Ptr201f(2),
				IDTokenInfo:      &messages.IdTokenInfoType{Status: "Accepted"},
			},
			Valid: true,
		},
		{
			Name: "valid without idTokenInfo",
			Message: messages.TransactionEventResponse{
				TotalCost:        decimalPtr201f("8.42"),
				ChargingPriority: int32Ptr201f(2),
			},
			Valid: true,
		},
		{
			Name: "valid without chargingPriority",
			Message: messages.TransactionEventResponse{
				TotalCost: decimalPtr201f("8.42"),
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
				TotalCost:              decimalPtr201f("8.42"),
				ChargingPriority:       int32Ptr201f(2),
				IDTokenInfo:            &messages.IdTokenInfoType{Status: "invalidAuthorizationStatus"},
				UpdatedPersonalMessage: &messages.MessageContentType{Format: "UTF8", Content: "dummyContent"},
			},
			Valid: false,
		},
		{
			Name: "invalid updatedPersonalMessage missing required fields",
			Message: map[string]any{
				"totalCost":              8.42,
				"chargingPriority":       2,
				"idTokenInfo":            map[string]any{"status": "Accepted"},
				"updatedPersonalMessage": map[string]any{},
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for totalCost minimum.
		// TODO(parity): needs schema override for chargingPriority minimum.
		// TODO(parity): needs schema override for chargingPriority maximum.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTransactionEvent201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201f(t, v201profiles.TransactionEvent)
}
