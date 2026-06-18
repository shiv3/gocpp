package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestGetLog21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "GetLog", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetLogRequest{
				Log:       testLogParameters(),
				LogType:   "DiagnosticsLog",
				RequestID: 1,
			},
			Valid: true,
		},
		{
			Name: "missing logType",
			Message: map[string]any{
				"log": map[string]any{
					"remoteLocation": "https://example.invalid/log",
				},
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "missing requestId",
			Message: map[string]any{
				"log": map[string]any{
					"remoteLocation": "https://example.invalid/log",
				},
				"logType": "DiagnosticsLog",
			},
			Valid: false,
		},
		{
			Name: "missing log",
			Message: map[string]any{
				"logType":   "DiagnosticsLog",
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "missing log remoteLocation",
			Message: map[string]any{
				"log":       map[string]any{},
				"logType":   "DiagnosticsLog",
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength log remoteLocation",
			Message: messages.GetLogRequest{
				Log: messages.LogParametersType{
					RemoteLocation: longString(2001),
				},
				LogType:   "DiagnosticsLog",
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum logType",
			Message: messages.GetLogRequest{
				Log:       testLogParameters(),
				LogType:   "invalidLogType",
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLog21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "GetLog", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.GetLogResponse{
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
			Name: "exceeds maxLength filename",
			Message: messages.GetLogResponse{
				Filename: stringPtr(longString(256)),
				Status:   "Accepted",
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.GetLogResponse{
				Status: "invalidLogStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLog21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.GetLog)
}
