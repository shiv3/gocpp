package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetDisplayMessages21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "GetDisplayMessages", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetDisplayMessagesRequest{
				RequestID: 1,
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "GetDisplayMessages", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetDisplayMessages21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "GetDisplayMessages", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetDisplayMessagesResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "GetDisplayMessages", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetDisplayMessages21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetDisplayMessages)
}
