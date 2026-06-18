package conf16b

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestStopTransaction16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "StopTransaction", "request")

	now := fixedTime16()
	reason := messages.StopTransactionRequestReasonEVDisconnected
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.StopTransactionRequest{
				IDTag:           ptr("12345"),
				MeterStop:       100,
				Timestamp:       now,
				TransactionID:   1,
				Reason:          &reason,
				TransactionData: []messages.TransactionData{transactionData16(now)},
			},
			Valid: true,
		},
		{
			Name: "valid empty transactionData",
			Message: messages.StopTransactionRequest{
				IDTag:           ptr("12345"),
				MeterStop:       100,
				Timestamp:       now,
				TransactionID:   1,
				Reason:          &reason,
				TransactionData: []messages.TransactionData{},
			},
			Valid: true,
		},
		{
			Name: "valid request without transactionData",
			Message: messages.StopTransactionRequest{
				IDTag:         ptr("12345"),
				MeterStop:     100,
				Timestamp:     now,
				TransactionID: 1,
				Reason:        &reason,
			},
			Valid: true,
		},
		{
			Name: "valid request without reason",
			Message: messages.StopTransactionRequest{
				IDTag:         ptr("12345"),
				MeterStop:     100,
				Timestamp:     now,
				TransactionID: 1,
			},
			Valid: true,
		},
		{
			Name: "valid request without idTag",
			Message: messages.StopTransactionRequest{
				MeterStop:     100,
				Timestamp:     now,
				TransactionID: 1,
			},
			Valid: true,
		},
		{
			Name: "valid zero transactionId",
			Message: messages.StopTransactionRequest{
				MeterStop: 100,
				Timestamp: now,
			},
			Valid: true,
		},
		{
			Name: "valid zero meterStop and transactionId",
			Message: messages.StopTransactionRequest{
				Timestamp: now,
			},
			Valid: true,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"meterStop": int32(100),
			},
			Valid: false,
		},
		{
			Name: "invalid reason enum",
			Message: messages.StopTransactionRequest{
				IDTag:         ptr("12345"),
				MeterStop:     100,
				Timestamp:     now,
				TransactionID: 1,
				Reason:        ptr(messages.StopTransactionRequestReason("invalidReason")),
			},
			Valid: false,
		},
		{
			Name: "invalid idTag exceeds maxLength 20",
			Message: messages.StopTransactionRequest{
				IDTag:         ptr(strings.Repeat("x", 21)),
				MeterStop:     100,
				Timestamp:     now,
				TransactionID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid transactionData empty sampledValue",
			Message: map[string]any{
				"meterStop":     int32(100),
				"timestamp":     now,
				"transactionId": int32(1),
				"transactionData": []map[string]any{
					{
						"timestamp":    now,
						"sampledValue": []map[string]any{},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStopTransaction16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "StopTransaction", "response")

	expiry := fixedTime16().Add(8 * time.Hour)
	cases := []conformance.ValidationCase{
		{
			Name: "valid response with idTagInfo",
			Message: messages.StopTransactionResponse{
				IDTagInfo: &messages.IDTagInfo{
					ExpiryDate:  &expiry,
					ParentIDTag: ptr("00000"),
					Status:      messages.IDTagInfoStatusAccepted,
				},
			},
			Valid: true,
		},
		{
			Name:    "valid empty response",
			Message: messages.StopTransactionResponse{},
			Valid:   true,
		},
		{
			Name: "invalid idTagInfo status enum",
			Message: messages.StopTransactionResponse{
				IDTagInfo: &messages.IDTagInfo{
					Status: messages.IDTagInfoStatus("invalidAuthorizationStatus"),
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStopTransaction16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.StopTransactionRequest, messages.StopTransactionResponse]{
		Action:    v16profiles.StopTransaction.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.StopTransactionRequest) (messages.StopTransactionResponse, error) {
		return messages.StopTransactionResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
