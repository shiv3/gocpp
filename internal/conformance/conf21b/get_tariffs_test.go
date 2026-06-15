package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetTariffs21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "GetTariffs", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetTariffsRequest{
				EVSEID: 1,
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "GetTariffs", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetTariffs21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "GetTariffs", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetTariffsResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "GetTariffs", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetTariffs21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetTariffs)
}
