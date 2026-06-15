package conf21b

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestChangeTransactionTariff21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "ChangeTransactionTariff", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ChangeTransactionTariffRequest{
				Tariff:        tariff21(),
				TransactionID: "transaction-1",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "ChangeTransactionTariff", "request")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestChangeTransactionTariff21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "ChangeTransactionTariff", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ChangeTransactionTariffResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
	}
	cases = append(cases, schemaGeneratedCases21(t, "ChangeTransactionTariff", "response")...)

	conformance.RunValidationTable(t, validator, cases)
}

func TestChangeTransactionTariff21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.ChangeTransactionTariff)
}
