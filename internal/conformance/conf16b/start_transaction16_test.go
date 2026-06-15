package conf16b

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestStartTransaction16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "StartTransaction", "request")

	now := fixedTime16()
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.StartTransactionRequest{
				ConnectorID:   1,
				IDTag:         "12345",
				MeterStart:    100,
				ReservationID: ptr(int32(42)),
				Timestamp:     now,
			},
			Valid: true,
		},
		{
			Name: "valid request without reservationId",
			Message: messages.StartTransactionRequest{
				ConnectorID: 1,
				IDTag:       "12345",
				MeterStart:  100,
				Timestamp:   now,
			},
			Valid: true,
		},
		{
			Name: "valid zero meterStart",
			Message: messages.StartTransactionRequest{
				ConnectorID: 1,
				IDTag:       "12345",
				Timestamp:   now,
			},
			Valid: true,
		},
		// TODO(parity): needs schema override for connectorId minimum.
		{
			Name: "invalid idTag exceeds maxLength 20",
			Message: messages.StartTransactionRequest{
				ConnectorID: 1,
				IDTag:       strings.Repeat("x", 21),
				MeterStart:  100,
				Timestamp:   now,
			},
			Valid: false,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"idTag":      "12345",
				"meterStart": int32(100),
				"timestamp":  now,
			},
			Valid: false,
		},
		{
			Name: "invalid missing idTag",
			Message: map[string]any{
				"connectorId": int32(1),
				"meterStart":  int32(100),
				"timestamp":   now,
			},
			Valid: false,
		},
		{
			Name: "invalid missing timestamp",
			Message: map[string]any{
				"connectorId": int32(1),
				"idTag":       "12345",
				"meterStart":  int32(100),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStartTransaction16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "StartTransaction", "response")

	expiry := fixedTime16().Add(8 * time.Hour)
	cases := []conformance.ValidationCase{
		{
			Name: "valid full response",
			Message: messages.StartTransactionResponse{
				IDTagInfo: messages.IDTagInfo{
					ExpiryDate:  &expiry,
					ParentIDTag: ptr("00000"),
					Status:      messages.IDTagInfoStatusAccepted,
				},
				TransactionID: 10,
			},
			Valid: true,
		},
		{
			Name: "valid zero transactionId",
			Message: messages.StartTransactionResponse{
				IDTagInfo: messages.IDTagInfo{
					ExpiryDate:  &expiry,
					ParentIDTag: ptr("00000"),
					Status:      messages.IDTagInfoStatusAccepted,
				},
			},
			Valid: true,
		},
		{
			Name: "invalid idTagInfo status enum",
			Message: messages.StartTransactionResponse{
				IDTagInfo: messages.IDTagInfo{
					Status: messages.IDTagInfoStatus("invalidAuthorizationStatus"),
				},
				TransactionID: 10,
			},
			Valid: false,
		},
		{
			Name: "invalid missing idTagInfo",
			Message: map[string]any{
				"transactionId": int32(10),
			},
			Valid: false,
		},
		{
			Name: "invalid missing transactionId",
			Message: map[string]any{
				"idTagInfo": messages.IDTagInfo{
					Status: messages.IDTagInfoStatusAccepted,
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStartTransaction16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.StartTransactionRequest, messages.StartTransactionResponse]{
		Action:    v16profiles.StartTransaction.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.StartTransactionRequest) (messages.StartTransactionResponse, error) {
		return messages.StartTransactionResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
