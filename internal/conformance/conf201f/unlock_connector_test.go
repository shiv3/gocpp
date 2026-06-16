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
		{
			Name: "invalid missing evseId",
			Message: map[string]any{
				"connectorId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"evseId": 2,
			},
			Valid: false,
		},
		{
			Name: "invalid evseId below minimum",
			Message: map[string]any{
				"evseId":      -1,
				"connectorId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"evseId":      2,
				"connectorId": -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
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
				StatusInfo: testStatusInfo201f(),
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
				StatusInfo: testStatusInfo201f(),
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
		{
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Unlocked",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestUnlockConnector201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201f(t, v201profiles.UnlockConnector)
}
