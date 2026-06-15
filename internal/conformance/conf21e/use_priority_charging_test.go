package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestUsePriorityCharging21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "UsePriorityCharging", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UsePriorityChargingRequest{
				Activate:      true,
				TransactionID: "transaction-1",
			},
			Valid: true,
		},
		{
			Name: "missing transactionId",
			Message: map[string]any{
				"activate": true,
			},
			Valid: false,
		},
		{
			Name: "missing activate",
			Message: map[string]any{
				"transactionId": "transaction-1",
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength transactionId",
			Message: messages.UsePriorityChargingRequest{
				Activate:      true,
				TransactionID: longString(37),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUsePriorityCharging21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "UsePriorityCharging", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UsePriorityChargingResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength statusInfo reasonCode",
			Message: messages.UsePriorityChargingResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.UsePriorityChargingResponse{
				Status: "invalidPriorityChargingStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUsePriorityCharging21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.UsePriorityCharging)
}
