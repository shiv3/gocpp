package conf201c

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetTransactionStatus201_RequestValidation(t *testing.T) {
	validator := validator201(t, "GetTransactionStatus", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty request",
			Message: messages.GetTransactionStatusRequest{},
			Valid:   true,
		},
		{
			Name: "valid transactionId",
			Message: messages.GetTransactionStatusRequest{
				TransactionID: strPtr("12345"),
			},
			Valid: true,
		},
		{
			Name: "invalid transactionId exceeds maxLength 36",
			Message: messages.GetTransactionStatusRequest{
				TransactionID: strPtr(strings.Repeat("x", 37)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetTransactionStatus201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "GetTransactionStatus", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response with ongoingIndicator",
			Message: messages.GetTransactionStatusResponse{
				OngoingIndicator: boolPtr(true),
				MessagesInQueue:  true,
			},
			Valid: true,
		},
		{
			Name: "valid response without ongoingIndicator",
			Message: messages.GetTransactionStatusResponse{
				MessagesInQueue: true,
			},
			Valid: true,
		},
		{
			Name:    "valid zero-value response",
			Message: messages.GetTransactionStatusResponse{},
			Valid:   true,
		},
		{
			Name:    "invalid missing messagesInQueue",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetTransactionStatus201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.GetTransactionStatus)
}
