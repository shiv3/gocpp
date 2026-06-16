package conf201e

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestReserveNow201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ReserveNow", "request")

	expiry := fixedTime201e()
	connectorType := "cCCS1"
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.ReserveNowRequest{
				ConnectorType:  &connectorType,
				CustomData:     testCustomData201e(),
				EVSEID:         int32Ptr201e(1),
				ExpiryDateTime: expiry,
				GroupIDToken:   &messages.IdTokenType{IDToken: "1234", Type: "ISO15693"},
				ID:             42,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "valid request without groupIdToken",
			Message: messages.ReserveNowRequest{
				ConnectorType:  &connectorType,
				EVSEID:         int32Ptr201e(1),
				ExpiryDateTime: expiry,
				ID:             42,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "valid request without evseId",
			Message: messages.ReserveNowRequest{
				ConnectorType:  &connectorType,
				ExpiryDateTime: expiry,
				ID:             42,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "valid request without connectorType",
			Message: messages.ReserveNowRequest{
				ExpiryDateTime: expiry,
				ID:             42,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "valid zero id request",
			Message: messages.ReserveNowRequest{
				ExpiryDateTime: expiry,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: true,
		},
		{
			Name: "invalid missing idToken",
			Message: map[string]any{
				"id":             42,
				"expiryDateTime": expiry,
			},
			Valid: false,
		},
		{
			Name: "invalid missing expiryDateTime",
			Message: map[string]any{
				"id":      42,
				"idToken": testIDToken201e("1234", "KeyCode"),
			},
			Valid: false,
		},
		{
			Name: "invalid missing id",
			Message: map[string]any{
				"expiryDateTime": expiry,
				"idToken":        testIDToken201e("1234", "KeyCode"),
			},
			Valid: false,
		},
		{
			Name:    "invalid missing required fields",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid id below minimum",
			Message: map[string]any{
				"id":             -1,
				"expiryDateTime": expiry,
				"idToken":        testIDToken201e("1234", "KeyCode"),
			},
			Valid: false,
		},
		{
			Name: "invalid connectorType enum",
			Message: messages.ReserveNowRequest{
				ConnectorType:  strPtr201e("invalidConnectorType"),
				EVSEID:         int32Ptr201e(1),
				ExpiryDateTime: expiry,
				GroupIDToken:   &messages.IdTokenType{IDToken: "1234", Type: "ISO15693"},
				ID:             42,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: false,
		},
		{
			Name: "invalid evseId below minimum",
			Message: map[string]any{
				"id":             42,
				"evseId":         -1,
				"expiryDateTime": expiry,
				"idToken":        testIDToken201e("1234", "KeyCode"),
			},
			Valid: false,
		},
		{
			Name: "invalid idToken type enum",
			Message: messages.ReserveNowRequest{
				ConnectorType:  &connectorType,
				EVSEID:         int32Ptr201e(1),
				ExpiryDateTime: expiry,
				GroupIDToken:   &messages.IdTokenType{IDToken: "1234", Type: "ISO15693"},
				ID:             42,
				IDToken:        testIDToken201e("1234", "invalidIdToken"),
			},
			Valid: false,
		},
		{
			Name: "invalid groupIdToken type enum",
			Message: messages.ReserveNowRequest{
				ConnectorType:  &connectorType,
				EVSEID:         int32Ptr201e(1),
				ExpiryDateTime: expiry,
				GroupIDToken:   &messages.IdTokenType{IDToken: "1234", Type: "invalidIdToken"},
				ID:             42,
				IDToken:        testIDToken201e("1234", "KeyCode"),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReserveNow201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "ReserveNow", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.ReserveNowResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.ReserveNowResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing status",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.ReserveNowResponse{
				Status: "invalidReserveNowStatus",
			},
			Valid: false,
		},
		{
			Name: "invalid missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid empty statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"reasonCode": ""},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestReserveNow201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.ReserveNow)
}
