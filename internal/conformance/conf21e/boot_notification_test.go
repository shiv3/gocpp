package conf21e

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func testTime() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func stringPtr(s string) *string {
	return &s
}

func testIDToken() messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: "id-token",
		Type:    "Central",
	}
}

func testOCSPRequestData() messages.OCSPRequestDataType {
	return messages.OCSPRequestDataType{
		HashAlgorithm:  "SHA256",
		IssuerKeyHash:  "issuer-key-hash",
		IssuerNameHash: "issuer-name-hash",
		ResponderURL:   "https://example.invalid/ocsp",
		SerialNumber:   "serial",
	}
}

func testLogParameters() messages.LogParametersType {
	return messages.LogParametersType{
		RemoteLocation: "https://example.invalid/log",
	}
}

func testMessageContent() messages.MessageContentType {
	return messages.MessageContentType{
		Content: "Display message",
		Format:  "UTF8",
	}
}

func testMessageInfo() messages.MessageInfoType {
	return messages.MessageInfoType{
		ID:       1,
		Message:  testMessageContent(),
		Priority: "NormalCycle",
	}
}

func requireCPRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v21.SubProtocol))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func requireCSMSRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(v21.SubProtocol))
	defer srv.Close()
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func TestBootNotification21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "BootNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					Model:      "ACME-1",
					VendorName: "ACME",
				},
				Reason: "PowerUp",
			},
			Valid: true,
		},
		{
			Name: "missing reason",
			Message: map[string]any{
				"chargingStation": map[string]any{
					"model":      "ACME-1",
					"vendorName": "ACME",
				},
			},
			Valid: false,
		},
		{
			Name: "missing chargingStation",
			Message: map[string]any{
				"reason": "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "missing chargingStation model",
			Message: map[string]any{
				"chargingStation": map[string]any{
					"vendorName": "ACME",
				},
				"reason": "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "missing chargingStation vendorName",
			Message: map[string]any{
				"chargingStation": map[string]any{
					"model": "ACME-1",
				},
				"reason": "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength model",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					Model:      longString(21),
					VendorName: "ACME",
				},
				Reason: "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid enum reason",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					Model:      "ACME-1",
					VendorName: "ACME",
				},
				Reason: "invalidBootReason",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBootNotification21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "BootNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				Interval:    300,
				Status:      "Accepted",
			},
			Valid: true,
		},
		{
			Name: "missing currentTime",
			Message: map[string]any{
				"interval": 300,
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing interval",
			Message: map[string]any{
				"currentTime": testTime(),
				"status":      "Accepted",
			},
			Valid: false,
		},
		{
			Name: "missing status",
			Message: map[string]any{
				"currentTime": testTime(),
				"interval":    300,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength statusInfo reasonCode",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				Interval:    300,
				Status:      "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				Interval:    300,
				Status:      "invalidRegistrationStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBootNotification21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.BootNotification)
}
