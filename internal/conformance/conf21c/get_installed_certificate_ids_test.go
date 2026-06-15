package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestGetInstalledCertificateIds21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetInstalledCertificateIds", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetInstalledCertificateIdsRequest{
				CertificateType: []string{"CSMSRootCertificate"},
			},
			Valid: true,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.GetInstalledCertificateIdsRequest{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.GetInstalledCertificateIdsRequest{
				CertificateType: []string{"BogusCertificateType"},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetInstalledCertificateIds21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetInstalledCertificateIds", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetInstalledCertificateIdsResponse{
				CertificateHashDataChain: []messages.CertificateHashDataChainType{certificateHashDataChain21()},
				Status:                   "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.GetInstalledCertificateIdsResponse{
				CertificateHashDataChain: []messages.CertificateHashDataChainType{
					{
						CertificateHashData: messages.CertificateHashDataType{
							HashAlgorithm:  "SHA256",
							IssuerKeyHash:  "issuer-key-hash",
							IssuerNameHash: "issuer-name-hash",
							SerialNumber:   longString(41),
						},
						CertificateType: "CSMSRootCertificate",
					},
				},
				Status: "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.GetInstalledCertificateIdsResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetInstalledCertificateIds21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetInstalledCertificateIds)
}
