package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestOpenPeriodicEventStream21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "OpenPeriodicEventStream", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.OpenPeriodicEventStreamRequest{
				ConstantStreamData: constantStreamData21(),
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "OpenPeriodicEventStream", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestOpenPeriodicEventStream21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "OpenPeriodicEventStream", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.OpenPeriodicEventStreamResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "OpenPeriodicEventStream", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestOpenPeriodicEventStream21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.OpenPeriodicEventStream)
}
