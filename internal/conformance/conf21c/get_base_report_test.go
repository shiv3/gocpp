package conf21c

import (
	"testing"

	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestGetBaseReport21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetBaseReport", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetBaseReportRequest{
				ReportBase: "FullInventory",
				RequestID:  1,
			},
			Valid: true,
		},
		{
			Name: "missing reportBase",
			Message: map[string]any{
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.GetBaseReportRequest{
				CustomData: &messages.CustomDataType{VendorID: longString(256)},
				ReportBase: "FullInventory",
				RequestID:  1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.GetBaseReportRequest{
				ReportBase: "BogusReport",
				RequestID:  1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetBaseReport21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "GetBaseReport", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetBaseReportResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.GetBaseReportResponse{
				Status:     "Accepted",
				StatusInfo: statusInfo21(longString(21)),
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.GetBaseReportResponse{
				Status: "BogusStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetBaseReport21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.GetBaseReport)
}
