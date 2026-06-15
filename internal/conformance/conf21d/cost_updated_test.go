package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
)

func TestCostUpdated21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "CostUpdated", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.CostUpdatedRequest{
				TotalCost:     decimal.NewFromInt(42),
				TransactionID: "tx-1",
			},
			Valid: true,
		},
		{
			Name: "missing transactionId",
			Message: map[string]any{
				"totalCost": 42,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength transactionId",
			Message: messages.CostUpdatedRequest{
				TotalCost:     decimal.NewFromInt(42),
				TransactionID: longString(37),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCostUpdated21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "CostUpdated", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.CostUpdatedResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.CostUpdatedResponse{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCostUpdated21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.CostUpdated)
}
