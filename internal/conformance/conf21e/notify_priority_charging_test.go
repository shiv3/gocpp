package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyPriorityCharging21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyPriorityCharging", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyPriorityChargingRequest{
				Activated:     true,
				TransactionID: "transaction-1",
			},
			Valid: true,
		},
		{
			Name: "missing transactionId",
			Message: map[string]any{
				"activated": true,
			},
			Valid: false,
		},
		{
			Name: "missing activated",
			Message: map[string]any{
				"transactionId": "transaction-1",
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength transactionId",
			Message: messages.NotifyPriorityChargingRequest{
				Activated:     true,
				TransactionID: longString(37),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyPriorityCharging21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "NotifyPriorityCharging", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyPriorityChargingResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyPriorityCharging21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.NotifyPriorityCharging)
}
