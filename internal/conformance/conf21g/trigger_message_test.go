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

func TestTriggerMessage21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "TriggerMessage", "request")

	validPayload := map[string]any{
		"customData":       customDataMap21(),
		"customTrigger":    "VendorMessage",
		"evse":             map[string]any{"connectorId": 1, "customData": customDataMap21(), "id": 1},
		"requestedMessage": "CustomTrigger",
	}

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.TriggerMessageRequest{
				CustomData:       customData21(),
				CustomTrigger:    strPtr21("VendorMessage"),
				EVSE:             evse21(),
				RequestedMessage: "CustomTrigger",
			},
			Valid: true,
		},
		{Name: "missing requestedMessage", Message: without21(validPayload, "requestedMessage"), Valid: false},
		{Name: "missing evse.id", Message: without21(validPayload, "evse", "id"), Valid: false},
		{Name: "invalid requestedMessage enum", Message: with21(validPayload, "InvalidMessage", "requestedMessage"), Valid: false},
		{Name: "customTrigger exceeds maxLength", Message: with21(validPayload, strings.Repeat("x", 51), "customTrigger"), Valid: false},
		{Name: "missing customData.vendorId", Message: with21(validPayload, map[string]any{}, "customData"), Valid: false},
		{Name: "customData.vendorId exceeds maxLength", Message: with21(validPayload, map[string]any{"vendorId": strings.Repeat("x", 256)}, "customData"), Valid: false},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestTriggerMessage21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "TriggerMessage", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.TriggerMessageResponse{
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
			Message: messages.TriggerMessageResponse{
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
			Message: messages.TriggerMessageResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.additionalInfo exceeds maxLength",
			Message: messages.TriggerMessageResponse{
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

func TestTriggerMessage21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.TriggerMessage)
}
