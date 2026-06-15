package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func validTransactionEventRequest21() messages.TransactionEventRequest {
	return messages.TransactionEventRequest{
		EventType:       "Started",
		SeqNo:           0,
		Timestamp:       fixedTime21(),
		TriggerReason:   "Authorized",
		TransactionInfo: messages.TransactionType{TransactionID: "txn-1"},
	}
}

func TestTransactionEvent21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "TransactionEvent", "request")

	withBadEventType := validTransactionEventRequest21()
	withBadEventType.EventType = "NotAnEvent"

	withBadTrigger := validTransactionEventRequest21()
	withBadTrigger.TriggerReason = "NotAReason"

	withLongTxnID := validTransactionEventRequest21()
	withLongTxnID.TransactionInfo = messages.TransactionType{TransactionID: strings.Repeat("x", 37)}

	cases := []conformance.ValidationCase{
		{Name: "valid", Message: validTransactionEventRequest21(), Valid: true},
		{Name: "missing required fields", Message: map[string]any{}, Valid: false},
		{Name: "invalid enum eventType", Message: withBadEventType, Valid: false},
		{Name: "invalid enum triggerReason", Message: withBadTrigger, Valid: false},
		{Name: "exceeds maxLength transactionInfo.transactionId", Message: withLongTxnID, Valid: false},
	}
	conformance.RunValidationTable(t, validator, cases)
}

func TestTransactionEvent21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "TransactionEvent", "response")

	cases := []conformance.ValidationCase{
		{Name: "valid empty", Message: messages.TransactionEventResponse{}, Valid: true},
		{Name: "valid with chargingPriority", Message: messages.TransactionEventResponse{ChargingPriority: int32Ptr21(5)}, Valid: true},
		{Name: "exceeds maxLength customData.vendorId", Message: messages.TransactionEventResponse{CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)}}, Valid: false},
	}
	conformance.RunValidationTable(t, validator, cases)
}

func TestTransactionEvent21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.TransactionEvent)
}
