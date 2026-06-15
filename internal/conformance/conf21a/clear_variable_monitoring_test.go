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

func TestClearVariableMonitoring21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.ClearVariableMonitoringRequest{
				ID: []int32{1},
			},
			Valid: true,
		},
		{
			Name:    "invalid missing id",
			Message: map[string]any{},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "ClearVariableMonitoring", "request"), cases)
}

func TestClearVariableMonitoring21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						ID:     1,
						Status: "Accepted",
					},
				},
			},
			Valid: true,
		},
		{
			Name:    "invalid missing clearMonitoringResult",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid clearMonitoringResult missing status",
			Message: map[string]any{
				"clearMonitoringResult": []map[string]any{{"id": 1}},
			},
			Valid: false,
		},
		{
			Name: "invalid clearMonitoringResult missing id",
			Message: map[string]any{
				"clearMonitoringResult": []map[string]any{{"status": "Accepted"}},
			},
			Valid: false,
		},
		{
			Name: "invalid clearMonitoringResult.status enum",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						ID:     1,
						Status: "BadEnum",
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid clearMonitoringResult.statusInfo missing reasonCode",
			Message: map[string]any{
				"clearMonitoringResult": []map[string]any{
					{
						"id":         1,
						"status":     "Accepted",
						"statusInfo": map[string]any{},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid clearMonitoringResult.statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						ID:     1,
						Status: "Accepted",
						StatusInfo: &messages.StatusInfoType{
							ReasonCode: longString(21),
						},
					},
				},
			},
			Valid: false,
		},
		{
			Name: "invalid clearMonitoringResult.statusInfo.additionalInfo exceeds maxLength 1024",
			Message: messages.ClearVariableMonitoringResponse{
				ClearMonitoringResult: []messages.ClearMonitoringResultType{
					{
						ID:     1,
						Status: "Accepted",
						StatusInfo: &messages.StatusInfoType{
							AdditionalInfo: ptr(longString(1025)),
							ReasonCode:     "reason",
						},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "ClearVariableMonitoring", "response"), cases)
}

func TestClearVariableMonitoring21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.ClearVariableMonitoring)
}
