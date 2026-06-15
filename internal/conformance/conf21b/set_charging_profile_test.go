package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSetChargingProfile21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "SetChargingProfile", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetChargingProfileRequest{
				ChargingProfile: chargingProfile21(),
				EVSEID:          1,
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "SetChargingProfile", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetChargingProfile21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "SetChargingProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetChargingProfileResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "SetChargingProfile", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetChargingProfile21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.SetChargingProfile)
}
