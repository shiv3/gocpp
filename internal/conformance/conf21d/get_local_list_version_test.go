package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetLocalListVersion21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "GetLocalListVersion", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.GetLocalListVersionRequest{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.GetLocalListVersionRequest{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLocalListVersion21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "GetLocalListVersion", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetLocalListVersionResponse{
				VersionNumber: 1,
			},
			Valid: true,
		},
		{
			Name:    "missing versionNumber",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.GetLocalListVersionResponse{
				CustomData:    invalidCustomData(),
				VersionNumber: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLocalListVersion21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.GetLocalListVersion)
}
