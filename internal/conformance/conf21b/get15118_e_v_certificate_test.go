package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGet15118EVCertificate21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "Get15118EVCertificate", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.Get15118EVCertificateRequest{
				Action:                "Install",
				ExiRequest:            "exi-request",
				Iso15118SchemaVersion: "urn:iso:15118:2:2013:MsgDef",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "Get15118EVCertificate", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestGet15118EVCertificate21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "Get15118EVCertificate", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.Get15118EVCertificateResponse{
				ExiResponse: "exi-response",
				Status:      "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "Get15118EVCertificate", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestGet15118EVCertificate21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.Get15118EVCertificate)
}
