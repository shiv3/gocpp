package conf201a

import (
	"context"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func TestCostUpdated201_RequestValidation(t *testing.T) {
	oldDecimalJSON := decimal.MarshalJSONWithoutQuotes
	decimal.MarshalJSONWithoutQuotes = true
	t.Cleanup(func() {
		decimal.MarshalJSONWithoutQuotes = oldDecimalJSON
	})

	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "CostUpdated", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.CostUpdatedRequest{
				TotalCost:     decimal.NewFromFloat(24.6),
				TransactionID: "1234",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.CostUpdatedRequest{
				CustomData:    testCustomData(),
				TotalCost:     decimal.NewFromFloat(24.6),
				TransactionID: "1234",
			},
			Valid: true,
		},
		{
			Name: "invalid missing transactionId",
			Message: map[string]any{
				"totalCost": 24.6,
			},
			Valid: false,
		},
		{
			Name: "invalid missing totalCost",
			Message: map[string]any{
				"transactionId": "1234",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid transactionId exceeds maxLength 36",
			Message: messages.CostUpdatedRequest{
				TotalCost:     decimal.NewFromFloat(24.6),
				TransactionID: strings.Repeat("x", 37),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCostUpdated201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "CostUpdated", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.CostUpdatedResponse{},
			Valid:   true,
		},
		{
			Name: "valid full response",
			Message: messages.CostUpdatedResponse{
				CustomData: testCustomData(),
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCostUpdated201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.CostUpdatedRequest, messages.CostUpdatedResponse]{
		Action:    v201profiles.CostUpdated.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.CostUpdatedRequest) (messages.CostUpdatedResponse, error) {
		return messages.CostUpdatedResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
