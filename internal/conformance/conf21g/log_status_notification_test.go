package conf21g

import (
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestLogStatusNotification21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "LogStatusNotification", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.LogStatusNotificationRequest{
				CustomData: customData21(),
				RequestID:  int32Ptr21(1),
				Status:     "Uploading",
				StatusInfo: statusInfo21(),
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{"customData": customDataMap21(), "requestId": 1, "statusInfo": statusInfoMap21()},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.LogStatusNotificationRequest{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
		{
			Name: "missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Uploading",
				"statusInfo": map[string]any{"additionalInfo": "details", "customData": customDataMap21()},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.reasonCode exceeds maxLength",
			Message: messages.LogStatusNotificationRequest{
				Status:     "Uploading",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.additionalInfo exceeds maxLength",
			Message: messages.LogStatusNotificationRequest{
				Status: "Uploading",
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
				"status":     "Uploading",
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"status":     "Uploading",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestLogStatusNotification21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "LogStatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.LogStatusNotificationResponse{
				CustomData: customData21(),
			},
			Valid: true,
		},
		{
			Name:    "missing customData.vendorId",
			Message: map[string]any{"customData": map[string]any{}},
			Valid:   false,
		},
		{
			Name:    "customData.vendorId exceeds maxLength",
			Message: map[string]any{"customData": map[string]any{"vendorId": strings.Repeat("x", 256)}},
			Valid:   false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestLogStatusNotification21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.LogStatusNotification)
}
