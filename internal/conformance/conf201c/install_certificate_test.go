package conf201c

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestInstallCertificate201_RequestValidation(t *testing.T) {
	validator := validator201(t, "InstallCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid v2g root certificate",
			Message: messages.InstallCertificateRequest{
				CertificateType: "V2GRootCertificate",
				Certificate:     "0xdeadbeef",
			},
			Valid: true,
		},
		{
			Name: "valid mo root certificate",
			Message: messages.InstallCertificateRequest{
				CertificateType: "MORootCertificate",
				Certificate:     "0xdeadbeef",
			},
			Valid: true,
		},
		{
			Name: "valid csms root certificate",
			Message: messages.InstallCertificateRequest{
				CertificateType: "CSMSRootCertificate",
				Certificate:     "0xdeadbeef",
			},
			Valid: true,
		},
		{
			Name: "valid manufacturer root certificate",
			Message: messages.InstallCertificateRequest{
				CertificateType: "ManufacturerRootCertificate",
				Certificate:     "0xdeadbeef",
			},
			Valid: true,
		},
		{
			Name: "invalid missing certificate",
			Message: map[string]any{
				"certificateType": "ManufacturerRootCertificate",
			},
			Valid: false,
		},
		{
			Name: "invalid missing certificateType",
			Message: map[string]any{
				"certificate": "0xdeadbeef",
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown certificateType enum",
			Message: messages.InstallCertificateRequest{
				CertificateType: "invalidCertificateUse",
				Certificate:     "0xdeadbeef",
			},
			Valid: false,
		},
		{
			Name: "invalid certificate exceeds maxLength 5500",
			Message: messages.InstallCertificateRequest{
				CertificateType: "V2GRootCertificate",
				Certificate:     strings.Repeat("x", 5501),
			},
			Valid: false,
		},
		// NOTE(parity): intentionally NOT overridden. ocpp-go accepts CSOSubCA1/CSOSubCA2 here
		// because it reuses one broad CertificateUse enum across messages; the authoritative OCA
		// 2.0.1 schema restricts InstallCertificate.certificateType to 4 values, so gocpp follows
		// the spec and rejects them. This divergence is by design.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestInstallCertificate201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "InstallCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.InstallCertificateResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.InstallCertificateResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid failed response",
			Message: messages.InstallCertificateResponse{
				Status: "Failed",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.InstallCertificateResponse{
				Status: "invalidInstallCertificateStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestInstallCertificate201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.InstallCertificate)
}
