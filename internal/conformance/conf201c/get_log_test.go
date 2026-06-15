package conf201c

import (
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
)

func TestGetLog201_RequestValidation(t *testing.T) {
	validator := validator201(t, "GetLog", "request")
	latest := fixedTime201()
	oldest := latest.Add(-2 * time.Hour)
	logParameters := messages.LogParametersType{
		RemoteLocation:  "ftp://someurl/diagnostics/1",
		OldestTimestamp: &oldest,
		LatestTimestamp: &latest,
	}

	cases := []conformance.ValidationCase{
		{
			Name: "valid full diagnostics request",
			Message: messages.GetLogRequest{
				LogType:       "DiagnosticsLog",
				RequestID:     1,
				Retries:       int32Ptr(5),
				RetryInterval: int32Ptr(120),
				Log:           logParameters,
			},
			Valid: true,
		},
		{
			Name: "valid without retryInterval",
			Message: messages.GetLogRequest{
				LogType:   "DiagnosticsLog",
				RequestID: 1,
				Retries:   int32Ptr(5),
				Log:       logParameters,
			},
			Valid: true,
		},
		{
			Name: "valid without retries and retryInterval",
			Message: messages.GetLogRequest{
				LogType:   "DiagnosticsLog",
				RequestID: 1,
				Log:       logParameters,
			},
			Valid: true,
		},
		{
			Name: "valid security log type",
			Message: messages.GetLogRequest{
				LogType:   "SecurityLog",
				RequestID: 1,
				Log:       logParameters,
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.GetLogRequest{
				LogType: "DiagnosticsLog",
				Log:     logParameters,
			},
			Valid: true,
		},
		{
			Name: "invalid missing log",
			Message: map[string]any{
				"logType":   "DiagnosticsLog",
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing logType",
			Message: map[string]any{
				"requestId": 1,
				"log":       logParameters,
			},
			Valid: false,
		},
		{
			Name: "invalid missing requestId",
			Message: map[string]any{
				"logType": "DiagnosticsLog",
				"log":     logParameters,
			},
			Valid: false,
		},
		{
			Name:    "invalid empty request",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown logType enum",
			Message: messages.GetLogRequest{
				LogType:       "invalidLogType",
				RequestID:     1,
				Retries:       int32Ptr(5),
				RetryInterval: int32Ptr(120),
				Log:           logParameters,
			},
			Valid: false,
		},
		// TODO(parity): needs schema override for requestId/retries/retryInterval minimums.
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLog201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "GetLog", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with filename",
			Message: messages.GetLogResponse{
				Status:   "Accepted",
				Filename: strPtr("testFileName.log"),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response without filename",
			Message: messages.GetLogResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.GetLogResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid accepted canceled response",
			Message: messages.GetLogResponse{
				Status: "AcceptedCanceled",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid unknown status enum",
			Message: messages.GetLogResponse{
				Status: "invalidLogStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid filename exceeds maxLength 255",
			Message: messages.GetLogResponse{
				Status:   "Accepted",
				Filename: strPtr(strings.Repeat("x", 256)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLog201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.GetLog)
}
