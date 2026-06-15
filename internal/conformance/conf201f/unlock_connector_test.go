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

func TestUnlockConnector201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "UnlockConnector", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.UnlockConnectorRequest{
				EVSEID:      2,
				ConnectorID: 1,
			},
			Valid: true,
		},
		{
			Name: "valid zero connectorId",
			Message: messages.UnlockConnectorRequest{
				EVSEID: 2,
			},
			Valid: true,
		},
		{
			Name:    "valid zero evseId and connectorId",
			Message: messages.UnlockConnectorRequest{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
	skipSchemaOverride201(t, "invalid evseId below minimum")
	skipSchemaOverride201(t, "invalid connectorId below minimum")
}

func TestUnlockConnector201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "UnlockConnector", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid unlocked response with statusInfo",
			Message: messages.UnlockConnectorResponse{
				Status:     "Unlocked",
				StatusInfo: statusInfo201("200"),
			},
			Valid: true,
		},
		{
			Name: "valid unlocked response",
			Message: messages.UnlockConnectorResponse{
				Status: "Unlocked",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.UnlockConnectorResponse{
				Status:     "invalidUnlockStatus",
				StatusInfo: statusInfo201("200"),
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Unlocked",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnlockConnector201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.UnlockConnector)
}
