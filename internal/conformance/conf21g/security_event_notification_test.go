package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestSecurityEventNotification21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SecurityEventNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.SecurityEventNotificationRequest{
				CustomData: customData21(),
				TechInfo:   strPtr21("details"),
				Timestamp:  testTime21(),
				Type:       "FirmwareUpdated",
			},
			Valid: true,
		},
		{
			Name:    "missing type",
			Message: map[string]any{"customData": customDataMap21(), "techInfo": "details", "timestamp": testTime21().Format(timeFormatRFC3339Nano21)},
			Valid:   false,
		},
		{
			Name:    "missing timestamp",
			Message: map[string]any{"customData": customDataMap21(), "techInfo": "details", "type": "FirmwareUpdated"},
			Valid:   false,
		},
		{
			Name: "type exceeds maxLength",
			Message: messages.SecurityEventNotificationRequest{
				Timestamp: testTime21(),
				Type:      strings.Repeat("x", 51),
			},
			Valid: false,
		},
		{
			Name: "techInfo exceeds maxLength",
			Message: messages.SecurityEventNotificationRequest{
				TechInfo:  strPtr21(strings.Repeat("x", 256)),
				Timestamp: testTime21(),
				Type:      "FirmwareUpdated",
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"timestamp":  testTime21().Format(timeFormatRFC3339Nano21),
				"type":       "FirmwareUpdated",
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"timestamp":  testTime21().Format(timeFormatRFC3339Nano21),
				"type":       "FirmwareUpdated",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSecurityEventNotification21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "SecurityEventNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.SecurityEventNotificationResponse{
				CustomData: customData21(),
			},
			Valid: true,
		},
		{
			Name:    "missing customData.vendorId",
			Message: map[string]any{"customData": map[string]any{}},
			Valid:   false,
		},
		{
			Name:    "customData.vendorId exceeds maxLength",
			Message: map[string]any{"customData": map[string]any{"vendorId": strings.Repeat("x", 256)}},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSecurityEventNotification21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.SecurityEventNotification)
}
