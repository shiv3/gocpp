package conf201f

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestSignCertificate201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SignCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid charging station certificate",
			Message: messages.SignCertificateRequest{
				Csr:             "deadc0de",
				CertificateType: ptr("ChargingStationCertificate"),
			},
			Valid: true,
		},
		{
			Name: "valid v2g certificate",
			Message: messages.SignCertificateRequest{
				Csr:             "deadc0de",
				CertificateType: ptr("V2GCertificate"),
			},
			Valid: true,
		},
		{
			Name: "valid without certificateType",
			Message: messages.SignCertificateRequest{
				Csr: "deadc0de",
			},
			Valid: true,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid certificateType enum",
			Message: messages.SignCertificateRequest{
				Csr:             "deadc0de",
				CertificateType: ptr("invalidType"),
			},
			Valid: false,
		},
		{
			Name: "invalid csr exceeds maxLength 5500",
			Message: messages.SignCertificateRequest{
				Csr: longString(5501),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignCertificate201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SignCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SignCertificateResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo201("200"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SignCertificateResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid status enum",
			Message: messages.SignCertificateResponse{
				Status:     "invalidStatus",
				StatusInfo: statusInfo201("200"),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSignCertificate201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.SignCertificate)
}
