package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestUnpublishFirmware21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "UnpublishFirmware", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UnpublishFirmwareRequest{
				Checksum: "checksum",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "UnpublishFirmware", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnpublishFirmware21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "UnpublishFirmware", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UnpublishFirmwareResponse{
				Status: "Unpublished",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "UnpublishFirmware", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnpublishFirmware21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.UnpublishFirmware)
}
