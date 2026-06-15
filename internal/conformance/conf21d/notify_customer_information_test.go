package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyCustomerInformation21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyCustomerInformation", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        "customer-data",
				GeneratedAt: testTime(),
				RequestID:   1,
				SeqNo:       1,
			},
			Valid: true,
		},
		{
			Name: "missing data",
			Message: map[string]any{
				"generatedAt": testTime(),
				"requestId":   1,
				"seqNo":       1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength data",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        longString(513),
				GeneratedAt: testTime(),
				RequestID:   1,
				SeqNo:       1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyCustomerInformation21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyCustomerInformation", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.NotifyCustomerInformationResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.NotifyCustomerInformationResponse{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyCustomerInformation21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.NotifyCustomerInformation)
}
