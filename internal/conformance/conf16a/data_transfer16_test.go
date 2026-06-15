package conf16a

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestDataTransfer16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "DataTransfer", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid vendor only request",
			Message: messages.DataTransferRequest{
				VendorID: "12345",
			},
			Valid: true,
		},
		{
			Name: "valid messageId request",
			Message: messages.DataTransferRequest{
				MessageID: strPtr("6789"),
				VendorID:  "12345",
			},
			Valid: true,
		},
		{
			Name: "valid data request",
			Message: messages.DataTransferRequest{
				Data:      strPtr("mockData"),
				MessageID: strPtr("6789"),
				VendorID:  "12345",
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
				VendorID: strings.Repeat("x", 256),
			},
			Valid: false,
		},
		{
			Name: "invalid messageId exceeds maxLength 50",
			Message: messages.DataTransferRequest{
				MessageID: strPtr(strings.Repeat("x", 51)),
				VendorID:  "12345",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDataTransfer16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "DataTransfer", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.DataTransferResponse{
				Status: messages.DataTransferResponseStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.DataTransferResponse{
				Status: messages.DataTransferResponseStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "valid unknown messageId response",
			Message: messages.DataTransferResponse{
				Status: messages.DataTransferResponseStatusUnknownMessageId,
			},
			Valid: true,
		},
		{
			Name: "valid unknown vendorId response",
			Message: messages.DataTransferResponse{
				Status: messages.DataTransferResponseStatusUnknownVendorId,
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.DataTransferResponse{
				Status: messages.DataTransferResponseStatus("invalidDataTransferStatus"),
			},
			Valid: false,
		},
		{
			Name: "valid data response",
			Message: messages.DataTransferResponse{
				Data:   strPtr("mockData"),
				Status: messages.DataTransferResponseStatusAccepted,
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDataTransfer16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.DataTransferRequest, messages.DataTransferResponse]{
		Action:    v16profiles.DataTransfer.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.DataTransferRequest) (messages.DataTransferResponse, error) {
		return messages.DataTransferResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
