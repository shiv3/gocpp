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

func TestGetReport21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.GetReportRequest{
				ComponentCriteria: []string{"Active"},
				ComponentVariable: []messages.ComponentVariableType{
					{
						Component: messages.ComponentType{
							EVSE:     &messages.EVSEType{ID: 1},
							Instance: ptr("Main"),
							Name:     "ChargingStation",
						},
						Variable: &messages.VariableType{
							Instance: ptr("Actual"),
							Name:     "Available",
						},
					},
				},
				RequestID: 1,
			},
			Valid: true,
		},
		{
			Name:    "invalid missing requestId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid componentVariable missing component",
			Message: map[string]any{
				"componentVariable": []map[string]any{{}},
				"requestId":         1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable.component missing name",
			Message: map[string]any{
				"componentVariable": []map[string]any{{"component": map[string]any{}}},
				"requestId":         1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable.variable missing name",
			Message: map[string]any{
				"componentVariable": []map[string]any{
					{
						"component": map[string]any{"name": "ChargingStation"},
						"variable":  map[string]any{},
					},
				},
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentCriteria enum",
			Message: messages.GetReportRequest{
				ComponentCriteria: []string{"BadEnum"},
				RequestID:         1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable.component.name exceeds maxLength 50",
			Message: messages.GetReportRequest{
				ComponentVariable: []messages.ComponentVariableType{
					{Component: messages.ComponentType{Name: longString(51)}},
				},
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable.component.instance exceeds maxLength 50",
			Message: messages.GetReportRequest{
				ComponentVariable: []messages.ComponentVariableType{
					{Component: messages.ComponentType{Instance: ptr(longString(51)), Name: "ChargingStation"}},
				},
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable.variable.name exceeds maxLength 50",
			Message: messages.GetReportRequest{
				ComponentVariable: []messages.ComponentVariableType{
					{
						Component: messages.ComponentType{Name: "ChargingStation"},
						Variable:  &messages.VariableType{Name: longString(51)},
					},
				},
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable.variable.instance exceeds maxLength 50",
			Message: messages.GetReportRequest{
				ComponentVariable: []messages.ComponentVariableType{
					{
						Component: messages.ComponentType{Name: "ChargingStation"},
						Variable:  &messages.VariableType{Instance: ptr(longString(51)), Name: "Available"},
					},
				},
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "GetReport", "request"), cases)
}

func TestGetReport21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.GetReportResponse{
				Status: "Accepted",
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
			Message: messages.GetReportResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo missing reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.GetReportResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.additionalInfo exceeds maxLength 1024",
			Message: messages.GetReportResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: ptr(longString(1025)),
					ReasonCode:     "reason",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "GetReport", "response"), cases)
}

func TestGetReport21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.GetReport)
}
