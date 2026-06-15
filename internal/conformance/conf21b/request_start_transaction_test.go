package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestRequestStartTransaction21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "RequestStartTransaction", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.RequestStartTransactionRequest{
				IDToken:       idToken21(),
				RemoteStartID: 1,
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "RequestStartTransaction", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestRequestStartTransaction21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "RequestStartTransaction", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.RequestStartTransactionResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "RequestStartTransaction", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestRequestStartTransaction21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.RequestStartTransaction)
}
