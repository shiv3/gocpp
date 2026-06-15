package conf201c

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetReport201_RequestValidation(t *testing.T) {
	validator := validator201(t, "GetReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.GetReportRequest{
				RequestID:         42,
				ComponentCriteria: []string{"Active", "Enabled", "Available", "Problem"},
				ComponentVariable: []messages.ComponentVariableType{componentVariable201()},
			},
			Valid: true,
		},
		{
			Name: "valid no optional fields",
			Message: messages.GetReportRequest{
				RequestID: 42,
			},
			Valid: true,
		},
		{
			Name:    "valid zero requestId",
			Message: messages.GetReportRequest{},
			Valid:   true,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"componentCriteria": []string{"Active"},
			},
			Valid: false,
		},
		{
			Name: "invalid unknown componentCriteria enum",
			Message: messages.GetReportRequest{
				RequestID:         42,
				ComponentCriteria: []string{"invalidComponentCriterion"},
			},
			Valid: false,
		},
		{
			Name: "invalid too many componentCriteria items",
			Message: messages.GetReportRequest{
				RequestID:         42,
				ComponentCriteria: []string{"Active", "Enabled", "Available", "Problem", "Active"},
			},
			Valid: false,
		},
		{
			Name: "invalid componentVariable missing component",
			Message: map[string]any{
				"requestId": 42,
				"componentVariable": []any{
					map[string]any{
						"variable": map[string]any{
							"name":     "variable1",
							"instance": "instance1",
						},
					},
				},
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for requestId minimum and empty array minItems parity.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetReport201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "GetReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response",
			Message: messages.GetReportResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.GetReportResponse{
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

func TestGetReport201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.GetReport)
}
