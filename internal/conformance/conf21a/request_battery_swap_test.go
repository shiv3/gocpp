package conf21a

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestRequestBatterySwap21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.RequestBatterySwapRequest{
				IDToken: messages.IdTokenType{
					IDToken: "id-token",
					Type:    "Central",
				},
				RequestID: 1,
			},
			Valid: true,
		},
		{
			Name: "missing idToken",
			Message: map[string]any{
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "missing idToken.type",
			Message: map[string]any{
				"idToken":   map[string]any{"idToken": "id-token"},
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength idToken.idToken",
			Message: messages.RequestBatterySwapRequest{
				IDToken: messages.IdTokenType{
					IDToken: longString(256),
					Type:    "Central",
				},
				RequestID: 1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "RequestBatterySwap", "request"), cases)
}

func TestRequestBatterySwap21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.RequestBatterySwapResponse{
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
			Name: "exceeds maxLength statusInfo.reasonCode",
			Message: messages.RequestBatterySwapResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: longString(21)},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.RequestBatterySwapResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "RequestBatterySwap", "response"), cases)
}

func TestRequestBatterySwap21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection(t, v21profiles.RequestBatterySwap)
}
