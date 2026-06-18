package conf201d

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestReportChargingProfiles201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "ReportChargingProfiles", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.ReportChargingProfilesRequest{
				RequestID:           42,
				ChargingLimitSource: "CSO",
				Tbc:                 ptr(true),
				EVSEID:              1,
				ChargingProfile:     []messages.ChargingProfileType{chargingProfile201("TxDefaultProfile")},
			},
			Valid: true,
		},
		{
			Name: "valid without tbc",
			Message: messages.ReportChargingProfilesRequest{
				RequestID:           42,
				ChargingLimitSource: "CSO",
				EVSEID:              1,
				ChargingProfile:     []messages.ChargingProfileType{chargingProfile201("TxDefaultProfile")},
			},
			Valid: true,
		},
		{
			Name: "valid zero evseId",
			Message: messages.ReportChargingProfilesRequest{
				RequestID:           42,
				ChargingLimitSource: "CSO",
				ChargingProfile:     []messages.ChargingProfileType{chargingProfile201("TxDefaultProfile")},
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.ReportChargingProfilesRequest{
				ChargingLimitSource: "CSO",
				ChargingProfile:     []messages.ChargingProfileType{chargingProfile201("TxDefaultProfile")},
			},
			Valid: true,
		},
		{
			Name: "invalid empty chargingProfile",
			Message: map[string]any{
				"chargingLimitSource": "CSO",
				"chargingProfile":     []any{},
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingProfile",
			Message: map[string]any{
				"chargingLimitSource": "CSO",
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingLimitSource",
			Message: messages.ReportChargingProfilesRequest{
				ChargingProfile: []messages.ChargingProfileType{chargingProfile201("TxDefaultProfile")},
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid chargingLimitSource enum",
			Message: messages.ReportChargingProfilesRequest{
				RequestID:           42,
				ChargingLimitSource: "invalidChargingLimitSource",
				Tbc:                 ptr(true),
				EVSEID:              1,
				ChargingProfile:     []messages.ChargingProfileType{chargingProfile201("TxDefaultProfile")},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid requestId below minimum")
	skipSchemaOverride201(t, "invalid evseId below minimum")
	skipSchemaOverride201(t, "invalid chargingProfile.stackLevel below minimum")
}

func TestReportChargingProfiles201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "ReportChargingProfiles", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.ReportChargingProfilesResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReportChargingProfiles201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.ReportChargingProfiles)
}
