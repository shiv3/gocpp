package conf201f

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func testVPN201f() *messages.VPNType {
	return &messages.VPNType{
		Server:   "someServer",
		User:     "user1",
		Group:    strPtr201f("group1"),
		Password: "deadc0de",
		Key:      "deadbeef",
		Type:     "IPSec",
	}
}

func testAPN201f() *messages.APNType {
	return &messages.APNType{
		Apn:                     "internet.t-mobile",
		ApnUserName:             strPtr201f("user1"),
		ApnPassword:             strPtr201f("deadc0de"),
		SimPin:                  int32Ptr201f(1234),
		PreferredNetwork:        strPtr201f("26201"),
		UseOnlyPreferredNetwork: boolPtr201f(true),
		ApnAuthentication:       "AUTO",
	}
}

func testNetworkConnectionProfile201f() messages.NetworkConnectionProfileType {
	return messages.NetworkConnectionProfileType{
		OCPPVersion:     "OCPP20",
		OCPPTransport:   "JSON",
		OCPPCsmsURL:     "http://someUrl:8767",
		MessageTimeout:  30,
		SecurityProfile: 1,
		OCPPInterface:   "Wired0",
		Vpn:             testVPN201f(),
		Apn:             testAPN201f(),
	}
}

func testSetNetworkProfileRequest201f(data messages.NetworkConnectionProfileType) messages.SetNetworkProfileRequest {
	return messages.SetNetworkProfileRequest{
		ConfigurationSlot: 2,
		ConnectionData:    data,
	}
}

func TestSetNetworkProfile201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetNetworkProfile", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid full request",
			Message: testSetNetworkProfileRequest201f(testNetworkConnectionProfile201f()),
			Valid:   true,
		},
		{
			Name: "valid without apn",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "valid without vpn and apn",
			Message: testSetNetworkProfileRequest201f(messages.NetworkConnectionProfileType{
				OCPPVersion:     "OCPP20",
				OCPPTransport:   "JSON",
				OCPPCsmsURL:     "http://someUrl:8767",
				MessageTimeout:  30,
				SecurityProfile: 1,
				OCPPInterface:   "Wired0",
			}),
			Valid: true,
		},
		{
			Name: "valid zero securityProfile",
			Message: testSetNetworkProfileRequest201f(messages.NetworkConnectionProfileType{
				OCPPVersion:    "OCPP20",
				OCPPTransport:  "JSON",
				OCPPCsmsURL:    "http://someUrl:8767",
				MessageTimeout: 30,
				OCPPInterface:  "Wired0",
			}),
			Valid: true,
		},
		{
			Name: "valid zero messageTimeout",
			Message: testSetNetworkProfileRequest201f(messages.NetworkConnectionProfileType{
				OCPPVersion:     "OCPP20",
				OCPPTransport:   "JSON",
				OCPPCsmsURL:     "http://someUrl:8767",
				SecurityProfile: 1,
				OCPPInterface:   "Wired0",
			}),
			Valid: true,
		},
		{
			Name: "valid zero configurationSlot",
			Message: messages.SetNetworkProfileRequest{
				ConnectionData: messages.NetworkConnectionProfileType{
					OCPPVersion:     "OCPP20",
					OCPPTransport:   "JSON",
					OCPPCsmsURL:     "http://someUrl:8767",
					MessageTimeout:  30,
					SecurityProfile: 1,
					OCPPInterface:   "Wired0",
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing configurationSlot",
			Message: map[string]any{
				"connectionData": map[string]any{
					"ocppVersion":     "OCPP20",
					"ocppTransport":   "JSON",
					"ocppCsmsUrl":     "http://someUrl:8767",
					"messageTimeout":  30,
					"securityProfile": 1,
					"ocppInterface":   "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing connectionData",
			Message: map[string]any{
				"configurationSlot": 2,
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppInterface",
			Message: map[string]any{
				"configurationSlot": 2,
				"connectionData": map[string]any{
					"ocppVersion":     "OCPP20",
					"ocppTransport":   "JSON",
					"ocppCsmsUrl":     "http://someUrl:8767",
					"messageTimeout":  30,
					"securityProfile": 1,
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppCsmsUrl",
			Message: map[string]any{
				"configurationSlot": 2,
				"connectionData": map[string]any{
					"ocppVersion":     "OCPP20",
					"ocppTransport":   "JSON",
					"messageTimeout":  30,
					"securityProfile": 1,
					"ocppInterface":   "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppTransport",
			Message: map[string]any{
				"configurationSlot": 2,
				"connectionData": map[string]any{
					"ocppVersion":     "OCPP20",
					"ocppCsmsUrl":     "http://someUrl:8767",
					"messageTimeout":  30,
					"securityProfile": 1,
					"ocppInterface":   "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppVersion",
			Message: map[string]any{
				"configurationSlot": 2,
				"connectionData": map[string]any{
					"ocppTransport":   "JSON",
					"ocppCsmsUrl":     "http://someUrl:8767",
					"messageTimeout":  30,
					"securityProfile": 1,
					"ocppInterface":   "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing messageTimeout",
			Message: map[string]any{
				"configurationSlot": 2,
				"connectionData": map[string]any{
					"ocppVersion":     "OCPP20",
					"ocppTransport":   "JSON",
					"ocppCsmsUrl":     "http://someUrl:8767",
					"securityProfile": 1,
					"ocppInterface":   "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing securityProfile",
			Message: map[string]any{
				"configurationSlot": 2,
				"connectionData": map[string]any{
					"ocppVersion":    "OCPP20",
					"ocppTransport":  "JSON",
					"ocppCsmsUrl":    "http://someUrl:8767",
					"messageTimeout": 30,
					"ocppInterface":  "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid ocppVersion enum",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.OCPPVersion = "OCPP01"
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid ocppTransport enum",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.OCPPTransport = "ProtoBuf"
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid ocppCsmsUrl exceeds maxLength 512",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.OCPPCsmsURL = strings.Repeat("x", 513)
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid ocppInterface enum",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.OCPPInterface = "invalidInterface"
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid empty vpn",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn = &messages.VPNType{}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid empty apn",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn = &messages.APNType{}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "valid vpn without group",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.Group = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "invalid vpn missing type",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn = &messages.VPNType{
					Server:   "someServer",
					User:     "user1",
					Password: "deadc0de",
					Key:      "deadbeef",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn missing key",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn = &messages.VPNType{
					Server:   "someServer",
					User:     "user1",
					Password: "deadc0de",
					Type:     "IPSec",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn missing password",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn = &messages.VPNType{
					Server: "someServer",
					User:   "user1",
					Key:    "deadbeef",
					Type:   "IPSec",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn missing user",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn = &messages.VPNType{
					Server:   "someServer",
					Password: "deadc0de",
					Key:      "deadbeef",
					Type:     "IPSec",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn missing server",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn = &messages.VPNType{
					User:     "user1",
					Password: "deadc0de",
					Key:      "deadbeef",
					Type:     "IPSec",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn.server exceeds maxLength 512",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.Server = strings.Repeat("x", 513)
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn.user exceeds maxLength 20",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.User = strings.Repeat("x", 21)
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn.group exceeds maxLength 20",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.Group = strPtr201f(strings.Repeat("x", 21))
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn.password exceeds maxLength 20",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.Password = strings.Repeat("x", 21)
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn.key exceeds maxLength 255",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.Key = strings.Repeat("x", 256)
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid vpn.type enum",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Vpn.Type = "invalidType"
				return data
			}()),
			Valid: false,
		},
		{
			Name: "valid apn without useOnlyPreferredNetwork",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.UseOnlyPreferredNetwork = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "valid apn without preferredNetwork",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.PreferredNetwork = nil
				data.Apn.UseOnlyPreferredNetwork = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "valid apn without simPin",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.SimPin = nil
				data.Apn.PreferredNetwork = nil
				data.Apn.UseOnlyPreferredNetwork = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "valid apn without password",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.ApnPassword = nil
				data.Apn.SimPin = nil
				data.Apn.PreferredNetwork = nil
				data.Apn.UseOnlyPreferredNetwork = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "valid apn without username",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.ApnUserName = nil
				data.Apn.ApnPassword = nil
				data.Apn.SimPin = nil
				data.Apn.PreferredNetwork = nil
				data.Apn.UseOnlyPreferredNetwork = nil
				return data
			}()),
			Valid: true,
		},
		{
			Name: "valid minimal apn",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn = &messages.APNType{
					Apn:               "internet.t-mobile",
					ApnAuthentication: "AUTO",
				}
				return data
			}()),
			Valid: true,
		},
		{
			Name: "invalid apn missing authentication",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn = &messages.APNType{
					Apn: "internet.t-mobile",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid apn missing apn",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn = &messages.APNType{
					ApnAuthentication: "AUTO",
				}
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid apn.apn exceeds maxLength 512",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.Apn = strings.Repeat("x", 513)
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid apn.apnUserName exceeds maxLength 20",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.ApnUserName = strPtr201f(strings.Repeat("x", 21))
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid apn.apnPassword exceeds maxLength 20",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.ApnPassword = strPtr201f(strings.Repeat("x", 21))
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid apn.preferredNetwork exceeds maxLength 6",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.PreferredNetwork = strPtr201f(strings.Repeat("x", 7))
				return data
			}()),
			Valid: false,
		},
		{
			Name: "invalid apnAuthentication enum",
			Message: testSetNetworkProfileRequest201f(func() messages.NetworkConnectionProfileType {
				data := testNetworkConnectionProfile201f()
				data.Apn.ApnAuthentication = "invalidApnAuthentication"
				return data
			}()),
			Valid: false,
		},
		// TODO(parity): needs schema override for configurationSlot minimum.
		// TODO(parity): needs schema override for messageTimeout minimum.
		// TODO(parity): needs schema override for simPin minimum.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetNetworkProfile201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetNetworkProfile", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SetNetworkProfileResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201f(),
			},
			Valid: true,
		},
		{
			Name: "valid rejected response with statusInfo",
			Message: messages.SetNetworkProfileResponse{
				Status:     "Rejected",
				StatusInfo: testStatusInfo201f(),
			},
			Valid: true,
		},
		{
			Name: "valid failed response with statusInfo",
			Message: messages.SetNetworkProfileResponse{
				Status:     "Failed",
				StatusInfo: testStatusInfo201f(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SetNetworkProfileResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid status enum",
			Message: messages.SetNetworkProfileResponse{
				Status:     "invalidSetNetworkProfileStatus",
				StatusInfo: testStatusInfo201f(),
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for empty statusInfo.reasonCode minLength.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetNetworkProfile201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.SetNetworkProfile)
}
