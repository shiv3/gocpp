package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestDataTransfer201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid vendorId only",
			Message: messages.DataTransferRequest{
				VendorID: "12345",
			},
			Valid: true,
		},
		{
			Name: "valid with messageId",
			Message: messages.DataTransferRequest{
				VendorID:  "12345",
				MessageID: ptr("6789"),
			},
			Valid: true,
		},
		{
			Name: "valid with data",
			Message: messages.DataTransferRequest{
				VendorID:  "12345",
				MessageID: ptr("6789"),
				Data:      ptr("mockData"),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing vendorId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid vendorId exceeds maxLength 255",
			Message: messages.DataTransferRequest{
				VendorID: longString(256),
			},
			Valid: false,
		},
		{
			Name: "invalid messageId exceeds maxLength 50",
			Message: messages.DataTransferRequest{
				VendorID:  "12345",
				MessageID: ptr(longString(51)),
			},
			Valid: false,
		},
	}

	runValidation201(t, "DataTransfer", "request", cases)
}

func TestDataTransfer201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid accepted response",
			Message: messages.DataTransferResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.DataTransferResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid unknown message id response",
			Message: messages.DataTransferResponse{
				Status: "UnknownMessageId",
			},
			Valid: true,
		},
		{
			Name: "valid unknown vendor id response",
			Message: messages.DataTransferResponse{
				Status: "UnknownVendorId",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.DataTransferResponse{
				Status: "invalidDataTransferStatus",
			},
			Valid: false,
		},
		{
			Name: "valid with data",
			Message: messages.DataTransferResponse{
				Status: "Accepted",
				Data:   ptr("mockData"),
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	runValidation201(t, "DataTransfer", "response", cases)
}

func TestDataTransfer201_Direction(t *testing.T) {
	assertCSMSRejectsWrongDirection(t, v201profiles.DataTransfer)
}
