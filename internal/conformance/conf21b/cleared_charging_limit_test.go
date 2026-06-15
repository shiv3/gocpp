package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestClearedChargingLimit21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "ClearedChargingLimit", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ClearedChargingLimitRequest{
				ChargingLimitSource: "EMS",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "ClearedChargingLimit", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearedChargingLimit21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "ClearedChargingLimit", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.ClearedChargingLimitResponse{},
			Valid:   true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "ClearedChargingLimit", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestClearedChargingLimit21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.ClearedChargingLimit)
}
