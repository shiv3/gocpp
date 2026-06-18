package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
)

func TestTransactionEvent21_RequestValidation(t *testing.T) {
	useDecimalJSONWithoutQuotes21(t)

	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "TransactionEvent", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: validTransactionEventRequest21(),
			Valid:   true,
		},
		{
			Name: "missing eventType",
			Message: map[string]any{
				"timestamp":       fixedTime21(),
				"triggerReason":   "Authorized",
				"seqNo":           0,
				"transactionInfo": map[string]any{"transactionId": "tx-1"},
			},
			Valid: false,
		},
		{
			Name: "missing transactionInfo.transactionId",
			Message: map[string]any{
				"eventType":       "Started",
				"timestamp":       fixedTime21(),
				"triggerReason":   "Authorized",
				"seqNo":           0,
				"transactionInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength transactionInfo.transactionId",
			Message: messages.TransactionEventRequest{
				EventType:     "Started",
				SeqNo:         0,
				Timestamp:     fixedTime21(),
				TriggerReason: "Authorized",
				TransactionInfo: messages.TransactionType{
					TransactionID: strings.Repeat("x", 37),
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength costDetails.failureReason",
			Message: messages.TransactionEventRequest{
				CostDetails: &messages.CostDetailsType{
					FailureReason: stringPtr21(strings.Repeat("x", 501)),
					TotalCost: messages.TotalCostType{
						Currency:   "EUR",
						Total:      messages.TotalPriceType{},
						TypeOfCost: "NormalCost",
					},
					TotalUsage: messages.TotalUsageType{
						ChargingTime: 60,
						Energy:       decimal.NewFromInt(42),
						IdleTime:     0,
					},
				},
				EventType:     "Started",
				SeqNo:         0,
				Timestamp:     fixedTime21(),
				TriggerReason: "Authorized",
				TransactionInfo: messages.TransactionType{
					TransactionID: "tx-1",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum eventType",
			Message: messages.TransactionEventRequest{
				EventType:     "InvalidEvent",
				SeqNo:         0,
				Timestamp:     fixedTime21(),
				TriggerReason: "Authorized",
				TransactionInfo: messages.TransactionType{
					TransactionID: "tx-1",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum triggerReason",
			Message: messages.TransactionEventRequest{
				EventType:     "Started",
				SeqNo:         0,
				Timestamp:     fixedTime21(),
				TriggerReason: "InvalidReason",
				TransactionInfo: messages.TransactionType{
					TransactionID: "tx-1",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTransactionEvent21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "TransactionEvent", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.TransactionEventResponse{},
			Valid:   true,
		},
		{
			Name: "missing updatedPersonalMessage.content",
			Message: map[string]any{
				"updatedPersonalMessage": map[string]any{"format": "UTF8"},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength updatedPersonalMessage.content",
			Message: messages.TransactionEventResponse{
				UpdatedPersonalMessage: &messages.MessageContentType{
					Content: strings.Repeat("x", 1025),
					Format:  "UTF8",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum updatedPersonalMessage.format",
			Message: messages.TransactionEventResponse{
				UpdatedPersonalMessage: &messages.MessageContentType{
					Content: "message",
					Format:  "InvalidFormat",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTransactionEvent21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.TransactionEvent)
}

func validTransactionEventRequest21() messages.TransactionEventRequest {
	return messages.TransactionEventRequest{
		CostDetails: &messages.CostDetailsType{
			TotalCost: messages.TotalCostType{
				Currency:   "EUR",
				Total:      messages.TotalPriceType{},
				TypeOfCost: "NormalCost",
			},
			TotalUsage: messages.TotalUsageType{
				ChargingTime: 60,
				Energy:       decimal.NewFromInt(42),
				IdleTime:     0,
			},
		},
		EventType:     "Started",
		MeterValue:    []messages.MeterValueType{testMeterValue21()},
		SeqNo:         0,
		Timestamp:     fixedTime21(),
		TriggerReason: "Authorized",
		TransactionInfo: messages.TransactionType{
			TransactionID: "tx-1",
		},
	}
}
