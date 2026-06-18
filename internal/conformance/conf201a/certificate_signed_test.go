package conf201a

import (
	"context"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	"github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestCertificateSigned201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "CertificateSigned", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.CertificateSignedRequest{
				CertificateChain: "sampleCert",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.CertificateSignedRequest{
				CertificateChain: "sampleCert",
				CertificateType:  strPtr("ChargingStationCertificate"),
				CustomData:       testCustomData(),
			},
			Valid: true,
		},
		{
			Name: "invalid empty certificateChain",
			Message: map[string]any{
				"certificateChain": "",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing certificateChain",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid certificateType enum",
			Message: messages.CertificateSignedRequest{
				CertificateChain: "sampleCert",
				CertificateType:  strPtr("NotAStatus"),
			},
			Valid: false,
		},
		{
			Name: "invalid certificateChain exceeds maxLength 10000",
			Message: messages.CertificateSignedRequest{
				CertificateChain: strings.Repeat("x", 10001),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCertificateSigned201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "CertificateSigned", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal response",
			Message: messages.CertificateSignedResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.CertificateSignedResponse{
				CustomData: testCustomData(),
				Status:     "Rejected",
				StatusInfo: testStatusInfo(),
			},
			Valid: true,
		},
		{
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.CertificateSignedResponse{
				Status: "NotAStatus",
			},
			Valid: false,
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
			Name: "invalid statusInfo.additionalInfo exceeds maxLength 512",
			Message: messages.CertificateSignedResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: strPtr(strings.Repeat("x", 513)),
					ReasonCode:     "OK",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCertificateSigned201_Direction(t *testing.T) {
	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.CertificateSignedRequest, messages.CertificateSignedResponse]{
		Action:    profiles.CertificateSigned.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req messages.CertificateSignedRequest) (messages.CertificateSignedResponse, error) {
		return messages.CertificateSignedResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
