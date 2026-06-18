package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestSetMonitoringLevel21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "SetMonitoringLevel", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.SetMonitoringLevelRequest{
				CustomData: customData21(),
				Severity:   1,
			},
			Valid: true,
		},
		{
			Name:    "missing severity",
			Message: map[string]any{"customData": customDataMap21()},
			Valid:   false,
		},
		{
			Name:    "missing customData.vendorId",
			Message: map[string]any{"customData": map[string]any{}, "severity": 1},
			Valid:   false,
		},
		{
			Name:    "customData.vendorId exceeds maxLength",
			Message: map[string]any{"customData": map[string]any{"vendorId": strings.Repeat("x", 256)}, "severity": 1},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringLevel21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "SetMonitoringLevel", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.SetMonitoringLevelResponse{
				CustomData: customData21(),
				Status:     "Accepted",
				StatusInfo: statusInfo21(),
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{"customData": customDataMap21(), "statusInfo": statusInfoMap21()},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.SetMonitoringLevelResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
		{
			Name: "missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"additionalInfo": "details", "customData": customDataMap21()},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.reasonCode exceeds maxLength",
			Message: messages.SetMonitoringLevelResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.additionalInfo exceeds maxLength",
			Message: messages.SetMonitoringLevelResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: strPtr21(strings.Repeat("x", 1025)),
					ReasonCode:     "OK",
				},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"status":     "Accepted",
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"status":     "Accepted",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestSetMonitoringLevel21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.SetMonitoringLevel)
}
