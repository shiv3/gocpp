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

func TestInstallCertificate21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "InstallCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.InstallCertificateRequest{
				Certificate:     "certificate",
				CertificateType: "V2GRootCertificate",
			},
			Valid: true,
		},
		{
			Name: "missing certificate",
			Message: map[string]any{
				"certificateType": "V2GRootCertificate",
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength certificate",
			Message: messages.InstallCertificateRequest{
				Certificate:     strings.Repeat("x", 10001),
				CertificateType: "V2GRootCertificate",
			},
			Valid: false,
		},
		{
			Name: "invalid enum certificateType",
			Message: messages.InstallCertificateRequest{
				Certificate:     "certificate",
				CertificateType: "InvalidType",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestInstallCertificate21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "InstallCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.InstallCertificateResponse{
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
			Message: messages.InstallCertificateResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.InstallCertificateResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestInstallCertificate21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.InstallCertificate)
}
