package conf16b

import (
	"context"
	"errors"
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

func fixedTime16() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func sampledValue16(value string) messages.SampledValue {
	return messages.SampledValue{Value: value}
}

func meterValue16(ts time.Time) messages.MeterValue {
	return messages.MeterValue{
		Timestamp:    ts,
		SampledValue: []messages.SampledValue{sampledValue16("value")},
	}
}

func transactionData16(ts time.Time) messages.TransactionData {
	return messages.TransactionData{
		Timestamp:    ts,
		SampledValue: []messages.SampledValue{sampledValue16("value")},
	}
}

func TestMeterValues16_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "MeterValues", "request")

	now := fixedTime16()
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.MeterValuesRequest{
				ConnectorID:   1,
				TransactionID: ptr(int32(1)),
				MeterValue:    []messages.MeterValue{meterValue16(now)},
			},
			Valid: true,
		},
		{
			Name: "valid request without transactionId",
			Message: messages.MeterValuesRequest{
				ConnectorID: 1,
				MeterValue:  []messages.MeterValue{meterValue16(now)},
			},
			Valid: true,
		},
		{
			Name: "valid zero connectorId",
			Message: messages.MeterValuesRequest{
				ConnectorID: 0,
				MeterValue:  []messages.MeterValue{meterValue16(now)},
			},
			Valid: true,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"meterValue": []messages.MeterValue{meterValue16(now)},
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId": -1,
				"meterValue":  []messages.MeterValue{meterValue16(now)},
			},
			Valid: false,
		},
		{
			Name: "invalid empty meterValue",
			Message: messages.MeterValuesRequest{
				ConnectorID: 1,
				MeterValue:  []messages.MeterValue{},
			},
			Valid: false,
		},
		{
			Name: "invalid missing meterValue",
			Message: map[string]any{
				"connectorId": int32(1),
			},
			Valid: false,
		},
		{
			Name: "invalid empty sampledValue",
			Message: messages.MeterValuesRequest{
				ConnectorID: 1,
				MeterValue: []messages.MeterValue{
					{
						Timestamp:    now,
						SampledValue: []messages.SampledValue{},
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestMeterValues16_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "1.6", "MeterValues", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.MeterValuesResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestMeterValues16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.MeterValuesRequest, messages.MeterValuesResponse]{
		Action:    v16profiles.MeterValues.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.MeterValuesRequest) (messages.MeterValuesResponse, error) {
		return messages.MeterValuesResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
