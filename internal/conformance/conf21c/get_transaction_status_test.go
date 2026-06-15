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

func TestGetTransactionStatus21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetTransactionStatus", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetTransactionStatusRequest{
				TransactionID: ptr("transaction-1"),
			},
			Valid: true,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.GetTransactionStatusRequest{
				TransactionID: ptr(longString(37)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetTransactionStatus21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetTransactionStatus", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetTransactionStatusResponse{
				MessagesInQueue: false,
			},
			Valid: true,
		},
		{
			Name:    "missing messagesInQueue",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.GetTransactionStatusResponse{
				CustomData:      &messages.CustomDataType{VendorID: longString(256)},
				MessagesInQueue: false,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetTransactionStatus21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetTransactionStatus)
}
