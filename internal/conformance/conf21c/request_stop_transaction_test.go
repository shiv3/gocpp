package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestRequestStopTransaction21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "RequestStopTransaction", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.RequestStopTransactionRequest{
				TransactionID: "transaction-1",
			},
			Valid: true,
		},
		{
			Name:    "missing transactionId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.RequestStopTransactionRequest{
				TransactionID: longString(37),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRequestStopTransaction21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "RequestStopTransaction", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.RequestStopTransactionResponse{
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
			Name: "exceeds maxLength",
			Message: messages.RequestStopTransactionResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo21(longString(21)),
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.RequestStopTransactionResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRequestStopTransaction21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.RequestStopTransaction)
}
