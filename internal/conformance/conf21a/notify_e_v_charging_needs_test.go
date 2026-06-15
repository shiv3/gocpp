package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestNotifyEVChargingNeeds21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyEVChargingNeedsRequest{
				ChargingNeeds: messages.ChargingNeedsType{
					RequestedEnergyTransfer: "DC",
				},
				EVSEID: 1,
			},
			Valid: true,
		},
		{
			Name: "missing chargingNeeds",
			Message: map[string]any{
				"evseId": 1,
			},
			Valid: false,
		},
		{
			Name: "missing chargingNeeds.requestedEnergyTransfer",
			Message: map[string]any{
				"chargingNeeds": map[string]any{},
				"evseId":        1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength chargingNeeds.derChargingParameters.evInverterManufacturer",
			Message: messages.NotifyEVChargingNeedsRequest{
				ChargingNeeds: messages.ChargingNeedsType{
					DerChargingParameters: &messages.DERChargingParametersType{
						EVInverterManufacturer: ptr(longString(51)),
					},
					RequestedEnergyTransfer: "DC",
				},
				EVSEID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum chargingNeeds.requestedEnergyTransfer",
			Message: messages.NotifyEVChargingNeedsRequest{
				ChargingNeeds: messages.ChargingNeedsType{
					RequestedEnergyTransfer: "BadEnum",
				},
				EVSEID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "NotifyEVChargingNeeds", "request"), cases)
}

func TestNotifyEVChargingNeeds21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.NotifyEVChargingNeedsResponse{
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
			Message: messages.NotifyEVChargingNeedsResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: longString(21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.NotifyEVChargingNeedsResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "NotifyEVChargingNeeds", "response"), cases)
}

func TestNotifyEVChargingNeeds21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.NotifyEVChargingNeeds)
}
