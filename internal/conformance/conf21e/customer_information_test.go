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

func TestCustomerInformation21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "CustomerInformation", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.CustomerInformationRequest{
				Clear:              true,
				CustomerIdentifier: stringPtr("customer-1"),
				Report:             true,
				RequestID:          1,
			},
			Valid: true,
		},
		{
			Name: "missing requestId",
			Message: map[string]any{
				"clear":  true,
				"report": true,
			},
			Valid: false,
		},
		{
			Name: "missing report",
			Message: map[string]any{
				"clear":     true,
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "missing clear",
			Message: map[string]any{
				"report":    true,
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength customerIdentifier",
			Message: messages.CustomerInformationRequest{
				Clear:              true,
				CustomerIdentifier: stringPtr(longString(65)),
				Report:             true,
				RequestID:          1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum customerCertificate hashAlgorithm",
			Message: messages.CustomerInformationRequest{
				Clear: true,
				CustomerCertificate: &messages.CertificateHashDataType{
					HashAlgorithm:  "invalidHashAlgorithm",
					IssuerKeyHash:  "issuer-key-hash",
					IssuerNameHash: "issuer-name-hash",
					SerialNumber:   "serial",
				},
				Report:    true,
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCustomerInformation21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "CustomerInformation", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.CustomerInformationResponse{
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
			Message: messages.CustomerInformationResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.CustomerInformationResponse{
				Status: "invalidCustomerInformationStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCustomerInformation21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.CustomerInformation)
}
