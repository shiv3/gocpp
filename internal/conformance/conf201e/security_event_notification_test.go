package conf201e

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

func TestSecurityEventNotification201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SecurityEventNotification", "request")

	timestamp := fixedTime201e()
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.SecurityEventNotificationRequest{
				CustomData: testCustomData201e(),
				TechInfo:   strPtr201e("someTechInfo"),
				Timestamp:  timestamp,
				Type:       "type1",
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.SecurityEventNotificationRequest{
				Timestamp: timestamp,
				Type:      "type1",
			},
			Valid: true,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"type": "type1",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		// TODO(parity): needs schema override for type minLength.
		{
			Name: "invalid type exceeds maxLength 50",
			Message: messages.SecurityEventNotificationRequest{
				TechInfo:  strPtr201e("someTechInfo"),
				Timestamp: timestamp,
				Type:      strings.Repeat("x", 51),
			},
			Valid: false,
		},
		{
			Name: "invalid techInfo exceeds maxLength 255",
			Message: messages.SecurityEventNotificationRequest{
				TechInfo:  strPtr201e(strings.Repeat("x", 256)),
				Timestamp: timestamp,
				Type:      "type1",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSecurityEventNotification201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SecurityEventNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.SecurityEventNotificationResponse{},
			Valid:   true,
		},
		{
			Name: "valid full response",
			Message: messages.SecurityEventNotificationResponse{
				CustomData: testCustomData201e(),
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSecurityEventNotification201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201e(t, v201profiles.SecurityEventNotification)
}
