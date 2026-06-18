package conf201f

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestStatusNotification201_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "StatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201f(),
				ConnectorStatus: "Available",
				EVSEID:          1,
				ConnectorID:     1,
			},
			Valid: true,
		},
		{
			Name: "valid zero connectorId",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201f(),
				ConnectorStatus: "Available",
				EVSEID:          1,
			},
			Valid: true,
		},
		{
			Name: "valid zero evseId and connectorId",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201f(),
				ConnectorStatus: "Available",
			},
			Valid: true,
		},
		{
			Name: "invalid missing connectorStatus",
			Message: map[string]any{
				"timestamp":   fixedTime201f(),
				"evseId":      1,
				"connectorId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"connectorStatus": "Available",
				"evseId":          1,
				"connectorId":     1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing evseId",
			Message: map[string]any{
				"timestamp":       fixedTime201f(),
				"connectorStatus": "Available",
				"connectorId":     1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"timestamp":       fixedTime201f(),
				"connectorStatus": "Available",
				"evseId":          1,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid connectorStatus enum",
			Message: messages.StatusNotificationRequest{
				Timestamp:       fixedTime201f(),
				ConnectorStatus: "invalidConnectorStatus",
				EVSEID:          1,
				ConnectorID:     1,
			},
			Valid: false,
		},
		{
			Name: "invalid evseId below minimum",
			Message: map[string]any{
				"timestamp":       fixedTime201f(),
				"connectorStatus": "Available",
				"evseId":          -1,
				"connectorId":     1,
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"timestamp":       fixedTime201f(),
				"connectorStatus": "Available",
				"evseId":          1,
				"connectorId":     -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification201_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.0.1", "StatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.StatusNotificationResponse{},
			Valid:   true,
		},
		{
			Name: "valid full response",
			Message: messages.StatusNotificationResponse{
				CustomData: testCustomData201f(),
			},
			Valid: true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201f(t, v201profiles.StatusNotification)
}
