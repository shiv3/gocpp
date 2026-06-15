package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestStatusNotification21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "StatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.StatusNotificationRequest{
				ConnectorID:     1,
				ConnectorStatus: "Available",
				EVSEID:          1,
				Timestamp:       testTime(),
			},
			Valid: true,
		},
		{
			Name: "missing timestamp",
			Message: map[string]any{
				"connectorId":     1,
				"connectorStatus": "Available",
				"evseId":          1,
			},
			Valid: false,
		},
		{
			Name: "missing connectorStatus",
			Message: map[string]any{
				"connectorId": 1,
				"evseId":      1,
				"timestamp":   testTime(),
			},
			Valid: false,
		},
		{
			Name: "missing evseId",
			Message: map[string]any{
				"connectorId":     1,
				"connectorStatus": "Available",
				"timestamp":       testTime(),
			},
			Valid: false,
		},
		{
			Name: "missing connectorId",
			Message: map[string]any{
				"connectorStatus": "Available",
				"evseId":          1,
				"timestamp":       testTime(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum connectorStatus",
			Message: messages.StatusNotificationRequest{
				ConnectorID:     1,
				ConnectorStatus: "invalidConnectorStatus",
				EVSEID:          1,
				Timestamp:       testTime(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "StatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.StatusNotificationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.StatusNotification)
}
