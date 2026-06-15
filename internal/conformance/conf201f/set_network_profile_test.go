package conf201f

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const subprotocol201 = "ocpp2.0.1"

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func ptr[T any](v T) *T {
	return &v
}

func fixedTime201() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func dec(v string) decimal.Decimal {
	d, err := decimal.NewFromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

func statusInfo201(reasonCode string) *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: reasonCode, AdditionalInfo: ptr("someInfo")}
}

func component201() messages.ComponentType {
	return messages.ComponentType{
		Name:     "component1",
		Instance: ptr("instance1"),
		EVSE: &messages.EVSEType{
			ID:          2,
			ConnectorID: ptr(int32(2)),
		},
	}
}

func variable201() messages.VariableType {
	return messages.VariableType{
		Name:     "variable1",
		Instance: ptr("instance1"),
	}
}

func messageContent201() messages.MessageContentType {
	return messages.MessageContentType{
		Format:  "UTF8",
		Content: "dummyContent",
	}
}

func idToken201(tokenType string) messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: "1234",
		Type:    tokenType,
	}
}

func requireCSMSHandlerInvalidDirection201[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(subprotocol201))
	defer srv.Close()
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var resp Resp
		return resp, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func requireCPHandlerInvalidDirection201[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(subprotocol201))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var resp Resp
		return resp, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func skipSchemaOverride201(t *testing.T, name string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		// TODO(parity): needs schema override
		t.Skip("constraint is not present in the bundled OCA schema")
	})
}

func vpn201() *messages.VPNType {
	return &messages.VPNType{
		Server:   "someServer",
		User:     "user1",
		Group:    ptr("group1"),
		Password: "deadc0de",
		Key:      "deadbeef",
		Type:     "IPSec",
	}
}

func apn201() *messages.APNType {
	return &messages.APNType{
		Apn:                     "internet.t-mobile",
		ApnUserName:             ptr("user1"),
		ApnPassword:             ptr("deadc0de"),
		SimPin:                  ptr(int32(1234)),
		PreferredNetwork:        ptr("26201"),
		UseOnlyPreferredNetwork: ptr(true),
		ApnAuthentication:       "AUTO",
	}
}

func networkConnectionProfile201() messages.NetworkConnectionProfileType {
	return messages.NetworkConnectionProfileType{
		OCPPVersion:     "OCPP20",
		OCPPTransport:   "JSON",
		OCPPCsmsURL:     "http://someUrl:8767",
		MessageTimeout:  30,
		SecurityProfile: 1,
		OCPPInterface:   "Wired0",
		Vpn:             vpn201(),
		Apn:             apn201(),
	}
}

func TestSetNetworkProfile201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetNetworkProfile", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData:    networkConnectionProfile201(),
			},
			Valid: true,
		},
		{
			Name: "valid without apn",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.Apn = nil
					return data
				}(),
			},
			Valid: true,
		},
		{
			Name: "valid without vpn and apn",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
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
			Name: "valid zero securityProfile",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: messages.NetworkConnectionProfileType{
					OCPPVersion:    "OCPP20",
					OCPPTransport:  "JSON",
					OCPPCsmsURL:    "http://someUrl:8767",
					MessageTimeout: 30,
					OCPPInterface:  "Wired0",
				},
			},
			Valid: true,
		},
		{
			Name: "valid zero messageTimeout",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: messages.NetworkConnectionProfileType{
					OCPPVersion:   "OCPP20",
					OCPPTransport: "JSON",
					OCPPCsmsURL:   "http://someUrl:8767",
					OCPPInterface: "Wired0",
				},
			},
			Valid: true,
		},
		{
			Name: "valid zero configurationSlot",
			Message: messages.SetNetworkProfileRequest{
				ConnectionData: messages.NetworkConnectionProfileType{
					OCPPVersion:   "OCPP20",
					OCPPTransport: "JSON",
					OCPPCsmsURL:   "http://someUrl:8767",
					OCPPInterface: "Wired0",
				},
			},
			Valid: true,
		},
		{
			Name: "invalid missing ocppInterface",
			Message: map[string]any{
				"connectionData": map[string]any{
					"ocppVersion":   "OCPP20",
					"ocppTransport": "JSON",
					"ocppCsmsUrl":   "http://someUrl:8767",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppCsmsUrl",
			Message: map[string]any{
				"connectionData": map[string]any{
					"ocppVersion":   "OCPP20",
					"ocppTransport": "JSON",
					"ocppInterface": "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppTransport",
			Message: map[string]any{
				"connectionData": map[string]any{
					"ocppVersion":   "OCPP20",
					"ocppCsmsUrl":   "http://someUrl:8767",
					"ocppInterface": "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing ocppVersion",
			Message: map[string]any{
				"connectionData": map[string]any{
					"ocppTransport": "JSON",
					"ocppCsmsUrl":   "http://someUrl:8767",
					"ocppInterface": "Wired0",
				},
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid ocppVersion enum",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.OCPPVersion = "OCPP01"
					return data
				}(),
			},
			Valid: false,
		},
		{
			Name: "invalid ocppTransport enum",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.OCPPTransport = "ProtoBuf"
					return data
				}(),
			},
			Valid: false,
		},
		{
			Name: "invalid ocppCsmsUrl exceeds maxLength 512",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.OCPPCsmsURL = longString(513)
					return data
				}(),
			},
			Valid: false,
		},
		{
			Name: "invalid ocppInterface enum",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.OCPPInterface = "invalidInterface"
					return data
				}(),
			},
			Valid: false,
		},
		{
			Name: "invalid empty vpn",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.Vpn = &messages.VPNType{}
					return data
				}(),
			},
			Valid: false,
		},
		{
			Name: "invalid empty apn",
			Message: messages.SetNetworkProfileRequest{
				ConfigurationSlot: 2,
				ConnectionData: func() messages.NetworkConnectionProfileType {
					data := networkConnectionProfile201()
					data.Apn = &messages.APNType{}
					return data
				}(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid configurationSlot below minimum")
	skipSchemaOverride201(t, "invalid messageTimeout below minimum")
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
				StatusInfo: statusInfo201("200"),
			},
			Valid: true,
		},
		{
			Name: "valid rejected response with statusInfo",
			Message: messages.SetNetworkProfileResponse{
				Status:     "Rejected",
				StatusInfo: statusInfo201("200"),
			},
			Valid: true,
		},
		{
			Name: "valid failed response with statusInfo",
			Message: messages.SetNetworkProfileResponse{
				Status:     "Failed",
				StatusInfo: statusInfo201("200"),
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
				StatusInfo: statusInfo201("200"),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetNetworkProfile201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.SetNetworkProfile)
}
