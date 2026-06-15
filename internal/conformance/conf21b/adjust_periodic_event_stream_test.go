package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestAdjustPeriodicEventStream21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "AdjustPeriodicEventStream", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.AdjustPeriodicEventStreamRequest{
				ID:     1,
				Params: periodicEventStreamParams21(),
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "AdjustPeriodicEventStream", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestAdjustPeriodicEventStream21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "AdjustPeriodicEventStream", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.AdjustPeriodicEventStreamResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "AdjustPeriodicEventStream", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestAdjustPeriodicEventStream21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.AdjustPeriodicEventStream)
}
