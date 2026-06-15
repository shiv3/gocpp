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

func TestSetNetworkProfile21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 1,
				ConnectionData: messages.NetworkConnectionProfileType{
					MessageTimeout:  30,
					OCPPCsmsURL:     "wss://csms.example/ocpp",
					OCPPInterface:   "Wired0",
					OCPPTransport:   "JSON",
					OCPPVersion:     ptr("OCPP21"),
					SecurityProfile: 1,
				},
			},
			Valid: true,
		},
		{
			Name: "missing connectionData",
			Message: map[string]any{
				"configurationSlot": 1,
			},
			Valid: false,
		},
		{
			Name: "missing connectionData.ocppCsmsUrl",
			Message: map[string]any{
				"configurationSlot": 1,
				"connectionData": map[string]any{
					"messageTimeout":  30,
					"ocppInterface":   "Wired0",
					"ocppTransport":   "JSON",
					"securityProfile": 1,
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength connectionData.ocppCsmsUrl",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 1,
				ConnectionData: messages.NetworkConnectionProfileType{
					MessageTimeout:  30,
					OCPPCsmsURL:     longString(2001),
					OCPPInterface:   "Wired0",
					OCPPTransport:   "JSON",
					SecurityProfile: 1,
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum connectionData.ocppInterface",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 1,
				ConnectionData: messages.NetworkConnectionProfileType{
					MessageTimeout:  30,
					OCPPCsmsURL:     "wss://csms.example/ocpp",
					OCPPInterface:   "BadEnum",
					OCPPTransport:   "JSON",
					SecurityProfile: 1,
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "SetNetworkProfile", "request"), cases)
}

func TestSetNetworkProfile21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.SetNetworkProfileResponse{
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
			Message: messages.SetNetworkProfileResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: longString(21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.SetNetworkProfileResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "SetNetworkProfile", "response"), cases)
}

func TestSetNetworkProfile21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.SetNetworkProfile)
}
