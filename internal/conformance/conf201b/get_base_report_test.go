package conf201b

import (
	"testing"

	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetBaseReport201_RequestValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid request",
			Message: messages.GetBaseReportRequest{
				RequestID:  42,
				ReportBase: "ConfigurationInventory",
			},
			Valid: true,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"reportBase": "ConfigurationInventory",
			},
			Valid: false,
		},
		{
			Name: "invalid missing reportBase",
			Message: map[string]any{
				"requestId": 42,
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid reportBase enum",
			Message: messages.GetBaseReportRequest{
				RequestID:  42,
				ReportBase: "invalidReportType",
			},
			Valid: false,
		},
	}

	runValidation201(t, "GetBaseReport", "request", cases)
}

func TestGetBaseReport201_ResponseValidation(t *testing.T) {
	cases := []validationCase{
		{
			Name: "valid accepted response",
			Message: messages.GetBaseReportResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.GetBaseReportResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid not supported response",
			Message: messages.GetBaseReportResponse{
				Status: "NotSupported",
			},
			Valid: true,
		},
		{
			Name: "valid empty result set response",
			Message: messages.GetBaseReportResponse{
				Status: "EmptyResultSet",
			},
			Valid: true,
		},
		{
			Name: "invalid status enum",
			Message: messages.GetBaseReportResponse{
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

	runValidation201(t, "GetBaseReport", "response", cases)
}

func TestGetBaseReport201_Direction(t *testing.T) {
	assertCPRejectsWrongDirection(t, v201profiles.GetBaseReport)
}
