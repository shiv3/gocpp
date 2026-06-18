package conf21a

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func TestAFRRSignal21_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.AFRRSignalRequest{
				Signal:    1,
				Timestamp: testTime(),
			},
			Valid: true,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"signal": 1,
			},
			Valid: false,
		},
		{
			Name: "invalid missing signal",
			Message: map[string]any{
				"timestamp": testTime(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "AFRRSignal", "request"), cases)
}

func TestAFRRSignal21_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v21.RegisterSchemas)

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.AFRRSignalResponse{
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
			Message: messages.AFRRSignalResponse{
				Status: "BadEnum",
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo missing reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.reasonCode exceeds maxLength 20",
			Message: messages.AFRRSignalResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: longString(21),
				},
			},
			Valid: false,
		},
		{
			Name: "invalid statusInfo.additionalInfo exceeds maxLength 1024",
			Message: messages.AFRRSignalResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: ptr(longString(1025)),
					ReasonCode:     "reason",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, conformance.MustValidator(t, reg, "2.1", "AFRRSignal", "response"), cases)
}

func TestAFRRSignal21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.AFRRSignal)
}

func requireCPRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(v21.SubProtocol))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func requireCSMSRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(v21.SubProtocol))
	defer srv.Close()
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

func ptr[T any](v T) *T {
	return &v
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func testTime() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func dec() decimal.Decimal {
	return decimal.NewFromInt(1)
}
