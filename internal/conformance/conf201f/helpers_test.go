package conf201f

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func strPtr201f(v string) *string {
	return &v
}

func int32Ptr201f(v int32) *int32 {
	return &v
}

func boolPtr201f(v bool) *bool {
	return &v
}

func fixedTime201f() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func testCustomData201f() *messages.CustomDataType {
	return &messages.CustomDataType{VendorID: "vendor"}
}

func testStatusInfo201f() *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: "200"}
}

func testComponent201f() messages.ComponentType {
	return messages.ComponentType{
		Name:     "component1",
		Instance: strPtr201f("instance1"),
		EVSE: &messages.EVSEType{
			ID:          2,
			ConnectorID: int32Ptr201f(2),
		},
	}
}

func testVariable201f() messages.VariableType {
	return messages.VariableType{
		Name:     "variable1",
		Instance: strPtr201f("instance1"),
	}
}

func decimal201f(v string) decimal.Decimal {
	d, err := decimal.NewFromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

func decimalPtr201f() *decimal.Decimal {
	d := decimal201f("8.42")
	return &d
}

func useDecimalJSONWithoutQuotes201f(t *testing.T) {
	t.Helper()

	oldDecimalJSON := decimal.MarshalJSONWithoutQuotes
	decimal.MarshalJSONWithoutQuotes = true
	t.Cleanup(func() {
		decimal.MarshalJSONWithoutQuotes = oldDecimalJSON
	})
}

func requireCSMSHandlerInvalidDirection201f[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
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

func requireCPHandlerInvalidDirection201f[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
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
