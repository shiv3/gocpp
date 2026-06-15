package conf21f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestReportChargingProfiles21_RequestValidation(t *testing.T) {
	useDecimalJSONWithoutQuotes21(t)

	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "ReportChargingProfiles", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ReportChargingProfilesRequest{
				ChargingLimitSource: "CSMS",
				ChargingProfile:     []messages.ChargingProfileType{testChargingProfile21()},
				EVSEID:              1,
				RequestID:           1,
			},
			Valid: true,
		},
		{
			Name: "missing chargingProfile",
			Message: map[string]any{
				"chargingLimitSource": "CSMS",
				"evseId":              1,
				"requestId":           1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength chargingLimitSource",
			Message: messages.ReportChargingProfilesRequest{
				ChargingLimitSource: strings.Repeat("x", 21),
				ChargingProfile:     []messages.ChargingProfileType{testChargingProfile21()},
				EVSEID:              1,
				RequestID:           1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum chargingProfile.chargingProfileKind",
			Message: messages.ReportChargingProfilesRequest{
				ChargingLimitSource: "CSMS",
				ChargingProfile: []messages.ChargingProfileType{
					{
						ID:                     1,
						StackLevel:             0,
						ChargingProfilePurpose: "ChargingStationMaxProfile",
						ChargingProfileKind:    "InvalidKind",
						ChargingSchedule:       testChargingProfile21().ChargingSchedule,
					},
				},
				EVSEID:    1,
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReportChargingProfiles21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "ReportChargingProfiles", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.ReportChargingProfilesResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.ReportChargingProfilesResponse{
				CustomData: &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReportChargingProfiles21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.ReportChargingProfiles)
}
