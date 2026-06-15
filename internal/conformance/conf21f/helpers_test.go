package conf21f

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	messages "github.com/shiv3/gocpp/v21/messages"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func stringPtr21(v string) *string {
	return &v
}

func int32Ptr21(v int32) *int32 {
	return &v
}

func decimalPtr21(v decimal.Decimal) *decimal.Decimal {
	return &v
}

func fixedTime21() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func useDecimalJSONWithoutQuotes21(t *testing.T) {
	t.Helper()

	oldDecimalJSON := decimal.MarshalJSONWithoutQuotes
	decimal.MarshalJSONWithoutQuotes = true
	t.Cleanup(func() {
		decimal.MarshalJSONWithoutQuotes = oldDecimalJSON
	})
}

func testStatusInfo21() *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: "OK"}
}

func testComponent21() messages.ComponentType {
	return messages.ComponentType{Name: "EVSE"}
}

func testVariable21() messages.VariableType {
	return messages.VariableType{Name: "Voltage"}
}

func testReportData21() messages.ReportDataType {
	attributeType := "Actual"
	return messages.ReportDataType{
		Component: testComponent21(),
		Variable:  testVariable21(),
		VariableAttribute: []messages.VariableAttributeType{
			{
				Type:  &attributeType,
				Value: stringPtr21("230"),
			},
		},
		VariableCharacteristics: &messages.VariableCharacteristicsType{
			DataType:           "decimal",
			SupportsMonitoring: true,
		},
	}
}

func testChargingProfile21() messages.ChargingProfileType {
	return messages.ChargingProfileType{
		ID:                     1,
		StackLevel:             0,
		ChargingProfilePurpose: "ChargingStationMaxProfile",
		ChargingProfileKind:    "Absolute",
		ChargingSchedule: []messages.ChargingScheduleType{
			{
				ID:               1,
				ChargingRateUnit: "W",
				ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{
					{
						StartPeriod: 0,
						Limit:       decimalPtr21(decimal.NewFromInt(10)),
					},
				},
			},
		},
	}
}

func testMeterValue21() messages.MeterValueType {
	return messages.MeterValueType{
		Timestamp: fixedTime21(),
		SampledValue: []messages.SampledValueType{
			{
				Value: decimal.NewFromInt(42),
			},
		},
	}
}

func testAddress21() *messages.AddressType {
	return &messages.AddressType{
		Name:     "Company",
		Address1: "Main Street 1",
		City:     "Amsterdam",
		Country:  "Netherlands",
	}
}

func requireCPRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols("ocpp2.1"))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func requireCSMSRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"))
	defer srv.Close()
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var zero Resp
		return zero, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
