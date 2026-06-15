package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestUnlockConnector21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UnlockConnectorRequest{
				ConnectorID: 1,
				EVSEID:      1,
			},
			Valid: true,
		},
		{
			Name: "missing connectorId",
			Message: map[string]any{
				"evseId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.UnlockConnectorRequest{
				ConnectorID: 1,
				CustomData:  &messages.CustomDataType{VendorID: longString(256)},
				EVSEID:      1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "UnlockConnector", "request"), cases)
}

func TestUnlockConnector21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.UnlockConnectorResponse{
				Status: "Unlocked",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.UnlockConnectorResponse{
				Status:     "Unlocked",
				StatusInfo: &messages.StatusInfoType{ReasonCode: longString(21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.UnlockConnectorResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "UnlockConnector", "response"), cases)
}

func TestUnlockConnector21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.UnlockConnector)
}
