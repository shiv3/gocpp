package conf21d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSetDefaultTariff21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "SetDefaultTariff", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetDefaultTariffRequest{
				EVSEID: 1,
				Tariff: testTariff(),
			},
			Valid: true,
		},
		{
			Name: "missing tariff",
			Message: map[string]any{
				"evseId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength currency",
			Message: messages.SetDefaultTariffRequest{
				EVSEID: 1,
				Tariff: messages.TariffType{
					Currency: "EURO",
					TariffID: "tariff-1",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum message format",
			Message: messages.SetDefaultTariffRequest{
				EVSEID: 1,
				Tariff: messages.TariffType{
					Currency: "EUR",
					Description: []messages.MessageContentType{
						{
							Content: "Tariff details",
							Format:  "InvalidMessageFormat",
						},
					},
					TariffID: "tariff-1",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDefaultTariff21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "SetDefaultTariff", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetDefaultTariffResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.SetDefaultTariffResponse{
				Status:     "Accepted",
				StatusInfo: invalidStatusInfoReason(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.SetDefaultTariffResponse{
				Status: "InvalidTariffSetStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetDefaultTariff21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.SetDefaultTariff)
}
