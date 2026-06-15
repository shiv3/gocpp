package conf201a

import (
	"context"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	"github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestBootNotification201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "BootNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{Model: "M1", VendorName: "Acme"},
				Reason:          "PowerUp",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					CustomData:      testCustomData(),
					FirmwareVersion: strPtr("1.2.3"),
					Model:           "M1",
					Modem: &messages.ModemType{
						CustomData: testCustomData(),
						ICCID:      strPtr("iccid"),
						IMSI:       strPtr("imsi"),
					},
					SerialNumber: strPtr("serial"),
					VendorName:   "Acme",
				},
				CustomData: testCustomData(),
				Reason:     "PowerUp",
			},
			Valid: true,
		},
		{
			Name: "invalid missing reason",
			Message: map[string]any{
				"chargingStation": map[string]any{
					"model":      "M1",
					"vendorName": "Acme",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingStation",
			Message: map[string]any{
				"reason": "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingStation.model",
			Message: map[string]any{
				"chargingStation": map[string]any{
					"vendorName": "Acme",
				},
				"reason": "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid missing chargingStation.vendorName",
			Message: map[string]any{
				"chargingStation": map[string]any{
					"model": "M1",
				},
				"reason": "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid reason enum",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{Model: "M1", VendorName: "Acme"},
				Reason:          "NotAStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid model exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{Model: strings.Repeat("x", 21), VendorName: "Acme"},
				Reason:          "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid vendorName exceeds maxLength 50",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{Model: "M1", VendorName: strings.Repeat("x", 51)},
				Reason:          "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid serialNumber exceeds maxLength 25",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					Model:        "M1",
					SerialNumber: strPtr(strings.Repeat("x", 26)),
					VendorName:   "Acme",
				},
				Reason: "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid firmwareVersion exceeds maxLength 50",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					FirmwareVersion: strPtr(strings.Repeat("x", 51)),
					Model:           "M1",
					VendorName:      "Acme",
				},
				Reason: "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid modem.iccid exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					Model:      "M1",
					Modem:      &messages.ModemType{ICCID: strPtr(strings.Repeat("x", 21))},
					VendorName: "Acme",
				},
				Reason: "PowerUp",
			},
			Valid: false,
		},
		{
			Name: "invalid modem.imsi exceeds maxLength 20",
			Message: messages.BootNotificationRequest{
				ChargingStation: messages.ChargingStationType{
					Model:      "M1",
					Modem:      &messages.ModemType{IMSI: strPtr(strings.Repeat("x", 21))},
					VendorName: "Acme",
				},
				Reason: "PowerUp",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBootNotification201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "BootNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal response",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				Interval:    60,
				Status:      "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				CustomData:  testCustomData(),
				Interval:    60,
				Status:      "Pending",
				StatusInfo:  testStatusInfo(),
			},
			Valid: true,
		},
		// TODO(parity): upstream rejects negative interval, but this schema has no minimum.
		{
			Name: "invalid missing currentTime",
			Message: map[string]any{
				"interval": 60,
				"status":   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid missing interval",
			Message: map[string]any{
				"currentTime": testTime(),
				"status":      "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid missing status",
			Message: map[string]any{
				"currentTime": testTime(),
				"interval":    60,
			},
			Valid: false,
		},
		{
			Name: "invalid status enum",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				Interval:    60,
				Status:      "NotAStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.BootNotificationResponse{
				CurrentTime: testTime(),
				Interval:    60,
				Status:      "Accepted",
				StatusInfo:  &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBootNotification201_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[messages.BootNotificationRequest, messages.BootNotificationResponse]{
		Action:    profiles.BootNotification.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.BootNotificationRequest) (messages.BootNotificationResponse, error) {
		return messages.BootNotificationResponse{}, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
