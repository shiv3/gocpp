package conf201f

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestStatusNotification201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "StatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201(),
				ConnectorStatus: "Available",
				EVSEID:          1,
				ConnectorID:     1,
			},
			Valid: true,
		},
		{
			Name: "valid zero connectorId",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201(),
				ConnectorStatus: "Available",
				EVSEID:          1,
			},
			Valid: true,
		},
		{
			Name: "valid zero evseId and connectorId",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201(),
				ConnectorStatus: "Available",
			},
			Valid: true,
		},
		{
			Name: "invalid missing connectorStatus",
			Message: map[string]any{
				"timestamp": fixedTime201(),
			},
			Valid: false,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"connectorStatus": "Available",
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid connectorStatus enum",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201(),
				ConnectorStatus: "invalidConnectorStatus",
				EVSEID:          1,
				ConnectorID:     1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid evseId below minimum")
	skipSchemaOverride201(t, "invalid connectorId below minimum")
}

func TestStatusNotification201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "StatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.StatusNotificationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.StatusNotification)
}
