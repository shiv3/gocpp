package conf21e

import (
	"testing"

	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
)

func TestReserveNow21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ReserveNow", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ReserveNowRequest{
				ExpiryDateTime: testTime(),
				ID:             1,
				IDToken:        testIDToken(),
			},
			Valid: true,
		},
		{
			Name: "missing id",
			Message: map[string]any{
				"expiryDateTime": testTime(),
				"idToken": map[string]any{
					"idToken": "id-token",
					"type":    "Central",
				},
			},
			Valid: false,
		},
		{
			Name: "missing expiryDateTime",
			Message: map[string]any{
				"id": 1,
				"idToken": map[string]any{
					"idToken": "id-token",
					"type":    "Central",
				},
			},
			Valid: false,
		},
		{
			Name: "missing idToken",
			Message: map[string]any{
				"expiryDateTime": testTime(),
				"id":             1,
			},
			Valid: false,
		},
		{
			Name: "missing idToken idToken",
			Message: map[string]any{
				"expiryDateTime": testTime(),
				"id":             1,
				"idToken": map[string]any{
					"type": "Central",
				},
			},
			Valid: false,
		},
		{
			Name: "missing idToken type",
			Message: map[string]any{
				"expiryDateTime": testTime(),
				"id":             1,
				"idToken": map[string]any{
					"idToken": "id-token",
				},
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength connectorType",
			Message: messages.ReserveNowRequest{
				ConnectorType:  stringPtr(longString(21)),
				ExpiryDateTime: testTime(),
				ID:             1,
				IDToken:        testIDToken(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReserveNow21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "2.1", "ReserveNow", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.ReserveNowResponse{
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
			Name: "exceeds maxLength statusInfo reasonCode",
			Message: messages.ReserveNowResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum status",
			Message: messages.ReserveNowResponse{
				Status: "invalidReserveNowStatus",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReserveNow21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.ReserveNow)
}
