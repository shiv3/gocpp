package conf201e

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func strPtr201e(v string) *string {
	return &v
}

func int32Ptr201e(v int32) *int32 {
	return &v
}

func fixedTime201e() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func testCustomData201e() *messages.CustomDataType {
	return &messages.CustomDataType{VendorID: "vendor"}
}

func testStatusInfo201e() *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: "200"}
}

func testIDToken201e(idToken, tokenType string) messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: idToken,
		Type:    tokenType,
	}
}

func testAuthorizationData201e() messages.AuthorizationData {
	return messages.AuthorizationData{
		IDToken: testIDToken201e("token1", "KeyCode"),
		IDTokenInfo: &messages.IdTokenInfoType{
			Status: "Accepted",
		},
	}
}

func testChargingProfile201e(stackLevel int32) messages.ChargingProfileType {
	return messages.ChargingProfileType{
		ID:                     1,
		StackLevel:             stackLevel,
		ChargingProfilePurpose: "ChargingStationMaxProfile",
		ChargingProfileKind:    "Absolute",
		ChargingSchedule: []messages.ChargingScheduleType{
			{
				ID:               1,
				ChargingRateUnit: "W",
				ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{
					{
						StartPeriod: 0,
						Limit:       decimal.NewFromFloat(200.0),
					},
				},
			},
		},
	}
}

func useDecimalJSONWithoutQuotes201e(t *testing.T) {
	t.Helper()

	oldDecimalJSON := decimal.MarshalJSONWithoutQuotes
	decimal.MarshalJSONWithoutQuotes = true
	t.Cleanup(func() {
		decimal.MarshalJSONWithoutQuotes = oldDecimalJSON
	})
}

func requireCSMSHandlerInvalidDirection201e[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var resp Resp
		return resp, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func requireCPHandlerInvalidDirection201e[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v201.SubProtocol))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var resp Resp
		return resp, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func TestRequestStopTransaction201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "RequestStopTransaction", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid minimal request",
			Message: messages.RequestStopTransactionRequest{
				TransactionID: "12345",
			},
			Valid: true,
		},
		{
			Name: "valid full request",
			Message: messages.RequestStopTransactionRequest{
				CustomData:    testCustomData201e(),
				TransactionID: "12345",
			},
			Valid: true,
		},
		{
			Name:    "invalid missing transactionId",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid transactionId exceeds maxLength 36",
			Message: messages.RequestStopTransactionRequest{
				TransactionID: strings.Repeat("x", 37),
			},
			Valid: false,
		},
		{
			Name: "invalid customData.vendorId exceeds maxLength 255",
			Message: messages.RequestStopTransactionRequest{
				CustomData:    &messages.CustomDataType{VendorID: strings.Repeat("x", 256)},
				TransactionID: "12345",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestRequestStopTransaction201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "RequestStopTransaction", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid accepted response with statusInfo",
			Message: messages.RequestStopTransactionResponse{
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
			},
			Valid: true,
		},
		{
			Name: "valid accepted response",
			Message: messages.RequestStopTransactionResponse{
				Status: "Accepted",
			},
			Valid: true,
		},
		{
			Name: "valid rejected response",
			Message: messages.RequestStopTransactionResponse{
				Status: "Rejected",
			},
			Valid: true,
		},
		{
			Name: "valid full response",
			Message: messages.RequestStopTransactionResponse{
				CustomData: testCustomData201e(),
				Status:     "Accepted",
				StatusInfo: testStatusInfo201e(),
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
			Message: messages.RequestStopTransactionResponse{
				Status:     "invalidRequestStartStopStatus",
				StatusInfo: testStatusInfo201e(),
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
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.RequestStopTransactionResponse{
				Status:     "Accepted",
				StatusInfo: &messages.StatusInfoType{ReasonCode: strings.Repeat("x", 21)},
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

func TestRequestStopTransaction201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201e(t, v201profiles.RequestStopTransaction)
}
