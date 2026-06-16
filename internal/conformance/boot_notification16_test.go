package conformance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestBootNotification16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "BootNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: "test",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.BootNotificationRequest{
				ChargeBoxSerialNumber:   strPtr("box"),
				ChargePointModel:        "test",
				ChargePointSerialNumber: strPtr("number"),
				ChargePointVendor:       "test",
				FirmwareVersion:         strPtr("version"),
				ICCID:                   strPtr("iccid"),
				IMSI:                    strPtr("imsi"),
				MeterSerialNumber:       strPtr("meter-serial"),
				MeterType:               strPtr("meter-type"),
			},
			Valid: true,
		},
		{
			Name: "invalid missing chargePointModel",
			Message: map[string]any{
				"chargeBoxSerialNumber":   "box",
				"chargePointSerialNumber": "number",
				"chargePointVendor":       "test",
				"firmwareVersion":         "version",
				"iccid":                   "iccid",
				"imsi":                    "imsi",
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargePointVendor",
			Message: map[string]any{
				"chargeBoxSerialNumber":   "box",
				"chargePointModel":        "test",
				"chargePointSerialNumber": "number",
				"firmwareVersion":         "version",
				"iccid":                   "iccid",
				"imsi":                    "imsi",
			},
			Valid: false,
		},
		{
			Name: "invalid chargeBoxSerialNumber exceeds maxLength 25",
			Message: messages.BootNotificationRequest{
				ChargeBoxSerialNumber: strPtr(strings.Repeat("x", 26)),
				ChargePointModel:      "test",
				ChargePointVendor:     "test",
			},
			Valid: false,
		},
		{
			Name: "invalid chargePointModel exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  strings.Repeat("x", 21),
				ChargePointVendor: "test",
			},
			Valid: false,
		},
		{
			Name: "invalid chargePointSerialNumber exceeds maxLength 25",
			Message: messages.BootNotificationRequest{
				ChargePointModel:        "test",
				ChargePointSerialNumber: strPtr(strings.Repeat("x", 26)),
				ChargePointVendor:       "test",
			},
			Valid: false,
		},
		{
			Name: "invalid chargePointVendor exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: strings.Repeat("x", 21),
			},
			Valid: false,
		},
		{
			Name: "invalid firmwareVersion exceeds maxLength 50",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: "test",
				FirmwareVersion:   strPtr(strings.Repeat("x", 51)),
			},
			Valid: false,
		},
		{
			Name: "invalid iccid exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: "test",
				ICCID:             strPtr(strings.Repeat("x", 21)),
			},
			Valid: false,
		},
		{
			Name: "invalid imsi exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: "test",
				IMSI:              strPtr(strings.Repeat("x", 21)),
			},
			Valid: false,
		},
		{
			Name: "invalid meterSerialNumber exceeds maxLength 25",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: "test",
				MeterSerialNumber: strPtr(strings.Repeat("x", 26)),
			},
			Valid: false,
		},
		{
			Name: "invalid meterType exceeds maxLength 25",
			Message: messages.BootNotificationRequest{
				ChargePointModel:  "test",
				ChargePointVendor: "test",
				MeterType:         strPtr(strings.Repeat("x", 26)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBootNotification16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "BootNotification", "response")

	now := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.BootNotificationResponse{
				CurrentTime: now,
				Interval:    60,
				Status:      messages.RegistrationStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "valid pending response",
			Message: messages.BootNotificationResponse{
				CurrentTime: now,
				Interval:    60,
				Status:      messages.RegistrationStatusPending,
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.BootNotificationResponse{
				CurrentTime: now,
				Interval:    60,
				Status:      messages.RegistrationStatusRejected,
			},
			Valid: true,
		},
		{
			Name: "valid interval zero",
			Message: messages.BootNotificationResponse{
				CurrentTime: now,
				Interval:    0,
				Status:      messages.RegistrationStatusAccepted,
			},
			Valid: true,
		},
		{
			Name: "invalid interval below minimum",
			Message: map[string]any{
				"currentTime": now,
				"interval":    -1,
				"status":      messages.RegistrationStatusAccepted,
			},
			Valid: false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.BootNotificationResponse{
				CurrentTime: now,
				Interval:    60,
				Status:      messages.RegistrationStatus("invalidRegistrationStatus"),
			},
			Valid: false,
		},
		{
			Name: "invalid missing status",
			Message: map[string]any{
				"currentTime": now,
				"interval":    60,
			},
			Valid: false,
		},
		{
			Name: "invalid missing currentTime",
			Message: map[string]any{
				"interval": 60,
				"status":   messages.RegistrationStatusAccepted,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBootNotification16_RoundTrip(t *testing.T) {
	resp, err := conformance.RoundTripCSMS(
		t,
		"ocpp1.6",
		nil,
		func(srv *csms.Server) {
			require.NoError(t, csms.On(srv, profiles.BootNotification, func(ctx context.Context, c *csms.Conn, req messages.BootNotificationRequest) (messages.BootNotificationResponse, error) {
				require.Equal(t, "Acme", req.ChargePointVendor)
				require.Equal(t, "M1", req.ChargePointModel)
				return messages.BootNotificationResponse{
					CurrentTime: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
					Interval:    300,
					Status:      messages.RegistrationStatusAccepted,
				}, nil
			}))
		},
		profiles.BootNotification,
		messages.BootNotificationRequest{
			ChargePointModel:  "M1",
			ChargePointVendor: "Acme",
		},
	)
	require.NoError(t, err)
	require.Equal(t, messages.RegistrationStatusAccepted, resp.Status)
	require.Equal(t, int32(300), resp.Interval)
}

func TestBootNotification16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.BootNotificationRequest, messages.BootNotificationResponse]{
		Action:    profiles.BootNotification.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.BootNotificationRequest) (messages.BootNotificationResponse, error) {
		return messages.BootNotificationResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func strPtr(s string) *string {
	return &s
}
