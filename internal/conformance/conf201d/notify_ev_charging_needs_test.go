package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func chargingNeeds201() messages.ChargingNeedsType {
	return messages.ChargingNeedsType{
		RequestedEnergyTransfer: "AC_three_phase",
		DepartureTime:           ptr(fixedTime201()),
		ACChargingParameters: &messages.ACChargingParametersType{
			EnergyAmount: 42,
			EVMinCurrent: 5,
			EVMaxCurrent: 10,
			EVMaxVoltage: 400,
		},
		DCChargingParameters: &messages.DCChargingParametersType{
			EVMaxCurrent:     0,
			EVMaxVoltage:     0,
			EnergyAmount:     ptr(int32(42)),
			EVMaxPower:       ptr(int32(150)),
			StateOfCharge:    ptr(int32(50)),
			EVEnergyCapacity: ptr(int32(42)),
			FullSoC:          ptr(int32(100)),
			BulkSoC:          ptr(int32(80)),
		},
	}
}

func TestNotifyEVChargingNeeds201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyEVChargingNeeds", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.NotifyEVChargingNeedsRequest{
				MaxScheduleTuples: ptr(int32(5)),
				EVSEID:            1,
				ChargingNeeds:     chargingNeeds201(),
			},
			Valid: true,
		},
		{
			Name: "valid without maxScheduleTuples",
			Message: messages.NotifyEVChargingNeedsRequest{
				EVSEID:        1,
				ChargingNeeds: chargingNeeds201(),
			},
			Valid: true,
		},
		{
			Name: "valid minimal chargingNeeds",
			Message: messages.NotifyEVChargingNeedsRequest{
				EVSEID: 1,
				ChargingNeeds: messages.ChargingNeedsType{
					RequestedEnergyTransfer: "AC_three_phase",
				},
			},
			Valid: true,
		},
		{
			Name: "valid ac mode without acChargingParameters",
			Message: messages.NotifyEVChargingNeedsRequest{
				EVSEID: 1,
				ChargingNeeds: messages.ChargingNeedsType{
					RequestedEnergyTransfer: "AC_three_phase",
					ACChargingParameters:    nil,
				},
			},
			Valid: true,
		},
		{
			Name: "valid dc mode without dcChargingParameters",
			Message: messages.NotifyEVChargingNeedsRequest{
				EVSEID: 1,
				ChargingNeeds: messages.ChargingNeedsType{
					RequestedEnergyTransfer: "DC",
					DCChargingParameters:    nil,
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing evseId",
			Message: map[string]any{
				"chargingNeeds": map[string]any{
					"requestedEnergyTransfer": "AC_three_phase",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingNeeds",
			Message: map[string]any{
				"evseId": 1,
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing chargingNeeds.requestedEnergyTransfer",
			Message: map[string]any{
				"evseId":        1,
				"chargingNeeds": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid requestedEnergyTransfer enum",
			Message: messages.NotifyEVChargingNeedsRequest{
				EVSEID: 1,
				ChargingNeeds: messages.ChargingNeedsType{
					RequestedEnergyTransfer: "invalidEnergyTransferMode",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid maxScheduleTuples below minimum")
	skipSchemaOverride201(t, "invalid acChargingParameters.energyAmount below minimum")
	skipSchemaOverride201(t, "invalid dcChargingParameters.evMaxCurrent below minimum")
}

func TestNotifyEVChargingNeeds201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyEVChargingNeeds", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.NotifyEVChargingNeedsResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo201("ok"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.NotifyEVChargingNeedsResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.NotifyEVChargingNeedsResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid processing response",
			Message: messages.NotifyEVChargingNeedsResponse{
				Status: "Processing",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.NotifyEVChargingNeedsResponse{
				Status: "invalidStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyEVChargingNeeds201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyEVChargingNeeds)
}
