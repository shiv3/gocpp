package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestNotifyAllowedEnergyTransfer21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyAllowedEnergyTransfer", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyAllowedEnergyTransferRequest{
				AllowedEnergyTransfer: []string{"DC"},
				TransactionID:         "transaction-1",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "NotifyAllowedEnergyTransfer", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyAllowedEnergyTransfer21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "NotifyAllowedEnergyTransfer", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyAllowedEnergyTransferResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "NotifyAllowedEnergyTransfer", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyAllowedEnergyTransfer21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.NotifyAllowedEnergyTransfer)
}
