package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetCertificateChainStatus21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "GetCertificateChainStatus", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetCertificateChainStatusRequest{
				CertificateStatusRequests: []messages.CertificateStatusRequestInfoType{
					{
						CertificateHashData: testCertificateHashData(),
						Source:              "OCSP",
						Urls:                []string{"https://example.com/status"},
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "missing certificateStatusRequests",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength serialNumber",
			Message: messages.GetCertificateChainStatusRequest{
				CertificateStatusRequests: []messages.CertificateStatusRequestInfoType{
					{
						CertificateHashData: messages.CertificateHashDataType{
							HashAlgorithm:  "SHA256",
							IssuerKeyHash:  "issuer-key-hash",
							IssuerNameHash: "issuer-name-hash",
							SerialNumber:   longString(41),
						},
						Source: "OCSP",
						Urls:   []string{"https://example.com/status"},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum hashAlgorithm",
			Message: messages.GetCertificateChainStatusRequest{
				CertificateStatusRequests: []messages.CertificateStatusRequestInfoType{
					{
						CertificateHashData: messages.CertificateHashDataType{
							HashAlgorithm:  "InvalidHashAlgorithm",
							IssuerKeyHash:  "issuer-key-hash",
							IssuerNameHash: "issuer-name-hash",
							SerialNumber:   "serial-1",
						},
						Source: "OCSP",
						Urls:   []string{"https://example.com/status"},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCertificateChainStatus21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "GetCertificateChainStatus", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetCertificateChainStatusResponse{
				CertificateStatus: []messages.CertificateStatusType{
					{
						CertificateHashData: testCertificateHashData(),
						NextUpdate:          testTime(),
						Source:              "OCSP",
						Status:              "Good",
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "missing certificateStatus",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength serialNumber",
			Message: messages.GetCertificateChainStatusResponse{
				CertificateStatus: []messages.CertificateStatusType{
					{
						CertificateHashData: messages.CertificateHashDataType{
							HashAlgorithm:  "SHA256",
							IssuerKeyHash:  "issuer-key-hash",
							IssuerNameHash: "issuer-name-hash",
							SerialNumber:   longString(41),
						},
						NextUpdate: testTime(),
						Source:     "OCSP",
						Status:     "Good",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.GetCertificateChainStatusResponse{
				CertificateStatus: []messages.CertificateStatusType{
					{
						CertificateHashData: testCertificateHashData(),
						NextUpdate:          testTime(),
						Source:              "OCSP",
						Status:              "InvalidCertificateStatus",
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetCertificateChainStatus21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.GetCertificateChainStatus)
}
