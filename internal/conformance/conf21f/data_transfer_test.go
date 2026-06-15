package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestDataTransfer21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "DataTransfer", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.DataTransferRequest{
				Data:      stringPtr21("payload"),
				MessageID: stringPtr21("message"),
				VendorID:  "vendor",
			},
			Valid: true,
		},
		{
			Name: "missing vendorId",
			Message: map[string]any{
				"messageId": "message",
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength messageId",
			Message: messages.DataTransferRequest{
				MessageID: stringPtr21(strings.Repeat("x", 51)),
				VendorID:  "vendor",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDataTransfer21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "DataTransfer", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.DataTransferResponse{
				Data:   stringPtr21("payload"),
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
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.DataTransferResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.DataTransferResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestDataTransfer21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.DataTransfer)
}
