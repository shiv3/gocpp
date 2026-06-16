package conf201e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestSetMonitoringLevel201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetMonitoringLevel", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid severity 0 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 0,
			},
			Valid: true,
		},
		{
			Name: "valid severity 1 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 1,
			},
			Valid: true,
		},
		{
			Name: "valid severity 2 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 2,
			},
			Valid: true,
		},
		{
			Name: "valid severity 3 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 3,
			},
			Valid: true,
		},
		{
			Name: "valid severity 4 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 4,
			},
			Valid: true,
		},
		{
			Name: "valid severity 5 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 5,
			},
			Valid: true,
		},
		{
			Name: "valid severity 6 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 6,
			},
			Valid: true,
		},
		{
			Name: "valid severity 7 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 7,
			},
			Valid: true,
		},
		{
			Name: "valid severity 8 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 8,
			},
			Valid: true,
		},
		{
			Name: "valid severity 9 request",
			Message: messages.SetMonitoringLevelRequest{
				Severity: 9,
			},
			Valid: true,
		},
		{
			Name:    "valid zero-value request",
			Message: messages.SetMonitoringLevelRequest{},
			Valid:   true,
		},
		{
			Name:    "invalid missing severity",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid severity below minimum",
			Message: map[string]any{
				"severity": -1,
			},
			Valid: false,
		},
		{
			Name: "invalid severity above maximum",
			Message: map[string]any{
				"severity": 10,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringLevel201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "SetMonitoringLevel", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.SetMonitoringLevelResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.SetMonitoringLevelResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.SetMonitoringLevelResponse{
				Status: "invalidDeviceModelStatus",
			},
			Valid: false,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringLevel201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.SetMonitoringLevel)
}
