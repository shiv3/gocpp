package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSignCertificate21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "SignCertificate", "request")

	certificateType := "InvalidCertificateSigningUse"
	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SignCertificateRequest{
				Csr: "certificate-signing-request",
			},
			Valid: true,
		},
		{
			Name:    "missing csr",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength csr",
			Message: messages.SignCertificateRequest{
				Csr: longString(5501),
			},
			Valid: false,
		},
		{
			Name: "invalid enum certificateType",
			Message: messages.SignCertificateRequest{
				CertificateType: &certificateType,
				Csr:             "certificate-signing-request",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignCertificate21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "SignCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SignCertificateResponse{
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
			Message: messages.SignCertificateResponse{
				Status:     "Accepted",
				StatusInfo: invalidStatusInfoReason(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.SignCertificateResponse{
				Status: "InvalidGenericStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignCertificate21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.SignCertificate)
}
