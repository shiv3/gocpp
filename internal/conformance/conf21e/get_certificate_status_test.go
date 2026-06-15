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

func TestGetCertificateStatus21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetCertificateStatus", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetCertificateStatusRequest{
				OcspRequestData: testOCSPRequestData(),
			},
			Valid: true,
		},
		{
			Name:    "missing ocspRequestData",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "missing ocspRequestData hashAlgorithm",
			Message: map[string]any{
				"ocspRequestData": map[string]any{
					"issuerKeyHash":  "issuer-key-hash",
					"issuerNameHash": "issuer-name-hash",
					"responderURL":   "https://example.invalid/ocsp",
					"serialNumber":   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "missing ocspRequestData issuerNameHash",
			Message: map[string]any{
				"ocspRequestData": map[string]any{
					"hashAlgorithm": "SHA256",
					"issuerKeyHash": "issuer-key-hash",
					"responderURL":  "https://example.invalid/ocsp",
					"serialNumber":  "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "missing ocspRequestData issuerKeyHash",
			Message: map[string]any{
				"ocspRequestData": map[string]any{
					"hashAlgorithm":  "SHA256",
					"issuerNameHash": "issuer-name-hash",
					"responderURL":   "https://example.invalid/ocsp",
					"serialNumber":   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "missing ocspRequestData serialNumber",
			Message: map[string]any{
				"ocspRequestData": map[string]any{
					"hashAlgorithm":  "SHA256",
					"issuerKeyHash":  "issuer-key-hash",
					"issuerNameHash": "issuer-name-hash",
					"responderURL":   "https://example.invalid/ocsp",
				},
			},
			Valid: false,
		},
		{
			Name: "missing ocspRequestData responderURL",
			Message: map[string]any{
				"ocspRequestData": map[string]any{
					"hashAlgorithm":  "SHA256",
					"issuerKeyHash":  "issuer-key-hash",
					"issuerNameHash": "issuer-name-hash",
					"serialNumber":   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength ocspRequestData responderURL",
			Message: messages.GetCertificateStatusRequest{
				OcspRequestData: messages.OCSPRequestDataType{
					HashAlgorithm:  "SHA256",
					IssuerKeyHash:  "issuer-key-hash",
					IssuerNameHash: "issuer-name-hash",
					ResponderURL:   longString(2001),
					SerialNumber:   "serial",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum ocspRequestData hashAlgorithm",
			Message: messages.GetCertificateStatusRequest{
				OcspRequestData: messages.OCSPRequestDataType{
					HashAlgorithm:  "invalidHashAlgorithm",
					IssuerKeyHash:  "issuer-key-hash",
					IssuerNameHash: "issuer-name-hash",
					ResponderURL:   "https://example.invalid/ocsp",
					SerialNumber:   "serial",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCertificateStatus21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetCertificateStatus", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetCertificateStatusResponse{
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
			Name: "exceeds maxLength ocspResult",
			Message: messages.GetCertificateStatusResponse{
				OcspResult: stringPtr(longString(18001)),
				Status:     "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.GetCertificateStatusResponse{
				Status: "invalidGetCertificateStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCertificateStatus21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.GetCertificateStatus)
}
