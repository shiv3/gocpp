package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func validNotifySettlementPayload21() map[string]any {
	return map[string]any{
		"customData":       customDataMap21(),
		"pspRef":           "psp-ref",
		"receiptId":        "receipt-1",
		"receiptUrl":       "https://example.com/receipt/1",
		"settlementAmount": 10,
		"settlementTime":   testTime21().Format(timeFormatRFC3339Nano21),
		"status":           "Settled",
		"statusInfo":       "settled",
		"transactionId":    "transaction-1",
		"vatCompany": map[string]any{
			"address1":   "Main Street 1",
			"address2":   "Suite 1",
			"city":       "Amsterdam",
			"country":    "Netherlands",
			"customData": customDataMap21(),
			"name":       "Example BV",
			"postalCode": "1000AA",
		},
		"vatNumber": "NL123456789",
	}
}

func TestNotifySettlement21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifySettlement", "request")

	validPayload := validNotifySettlementPayload21()

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.NotifySettlementRequest{
				CustomData:       customData21(),
				PspRef:           "psp-ref",
				ReceiptID:        strPtr21("receipt-1"),
				ReceiptURL:       strPtr21("https://example.com/receipt/1"),
				SettlementAmount: dec21(10),
				SettlementTime:   testTime21(),
				Status:           "Settled",
				StatusInfo:       strPtr21("settled"),
				TransactionID:    strPtr21("transaction-1"),
				VatCompany:       address21(),
				VatNumber:        strPtr21("NL123456789"),
			},
			Valid: true,
		},
		{Name: "missing pspRef", Message: without21(validPayload, "pspRef"), Valid: false},
		{Name: "missing status", Message: without21(validPayload, "status"), Valid: false},
		{Name: "missing settlementAmount", Message: without21(validPayload, "settlementAmount"), Valid: false},
		{Name: "missing settlementTime", Message: without21(validPayload, "settlementTime"), Valid: false},
		{Name: "invalid status enum", Message: with21(validPayload, "InvalidStatus", "status"), Valid: false},
		{Name: "pspRef exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 256), "pspRef"), Valid: false},
		{Name: "statusInfo exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 501), "statusInfo"), Valid: false},
		{Name: "transactionId exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 37), "transactionId"), Valid: false},
		{Name: "receiptId exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 51), "receiptId"), Valid: false},
		{Name: "receiptUrl exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 2001), "receiptUrl"), Valid: false},
		{Name: "vatNumber exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 21), "vatNumber"), Valid: false},
		{Name: "missing vatCompany.name", Message: without21(validPayload, "vatCompany", "name"), Valid: false},
		{Name: "missing vatCompany.address1", Message: without21(validPayload, "vatCompany", "address1"), Valid: false},
		{Name: "missing vatCompany.city", Message: without21(validPayload, "vatCompany", "city"), Valid: false},
		{Name: "missing vatCompany.country", Message: without21(validPayload, "vatCompany", "country"), Valid: false},
		{Name: "vatCompany.name exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 51), "vatCompany", "name"), Valid: false},
		{Name: "vatCompany.address1 exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 101), "vatCompany", "address1"), Valid: false},
		{Name: "vatCompany.address2 exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 101), "vatCompany", "address2"), Valid: false},
		{Name: "vatCompany.city exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 101), "vatCompany", "city"), Valid: false},
		{Name: "vatCompany.postalCode exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 21), "vatCompany", "postalCode"), Valid: false},
		{Name: "vatCompany.country exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 51), "vatCompany", "country"), Valid: false},
		{Name: "missing customData.vendorId", Message: with21(validPayload, map[string]any{}, "customData"), Valid: false},
		{Name: "customData.vendorId exceeds maxLength", Message: with21(validPayload, map[string]any{"vendorId": strings.Repeat("x", 256)}, "customData"), Valid: false},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifySettlement21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifySettlement", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.NotifySettlementResponse{
				CustomData: customData21(),
				ReceiptID:  strPtr21("receipt-1"),
				ReceiptURL: strPtr21("https://example.com/receipt/1"),
			},
			Valid: true,
		},
		{
			Name: "receiptId exceeds maxLength",
			Message: messages.NotifySettlementResponse{
				ReceiptID: strPtr21(strings.Repeat("x", 51)),
			},
			Valid: false,
		},
		{
			Name: "receiptUrl exceeds maxLength",
			Message: messages.NotifySettlementResponse{
				ReceiptURL: strPtr21(strings.Repeat("x", 2001)),
			},
			Valid: false,
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

func TestNotifySettlement21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.NotifySettlement)
}
