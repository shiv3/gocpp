package conf21d

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	"github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func must21Validator(t *testing.T, action, kind string) *schema.Validator {
	t.Helper()

	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	return conformance.MustValidator(t, reg, "2.1", action, kind)
}

func int32Ptr(i int32) *int32 {
	return &i
}

func testTime() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func invalidCustomData() *messages.CustomDataType {
	return &messages.CustomDataType{VendorID: longString(256)}
}

func invalidStatusInfoReason() *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: longString(21)}
}

func testIDToken() messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: "id-token-1",
		Type:    "ISO14443",
	}
}

func testBatteryData() messages.BatteryDataType {
	return messages.BatteryDataType{
		EVSEID:       1,
		SerialNumber: "battery-1",
		SoC:          decimal.NewFromInt(80),
		SoH:          decimal.NewFromInt(90),
	}
}

func testCertificateHashData() messages.CertificateHashDataType {
	return messages.CertificateHashDataType{
		HashAlgorithm:  "SHA256",
		IssuerKeyHash:  "issuer-key-hash",
		IssuerNameHash: "issuer-name-hash",
		SerialNumber:   "serial-1",
	}
}

func testComponent() messages.ComponentType {
	return messages.ComponentType{Name: "Connector"}
}

func testVariable() messages.VariableType {
	return messages.VariableType{Name: "AvailabilityState"}
}

func testVariableMonitoring() messages.VariableMonitoringType {
	return messages.VariableMonitoringType{
		EventNotificationType: "CustomMonitor",
		ID:                    1,
		Severity:              1,
		Transaction:           false,
		Type:                  "UpperThreshold",
		Value:                 decimal.NewFromInt(10),
	}
}

func testMonitoringData() messages.MonitoringDataType {
	return messages.MonitoringDataType{
		Component:          testComponent(),
		Variable:           testVariable(),
		VariableMonitoring: []messages.VariableMonitoringType{testVariableMonitoring()},
	}
}

func testTariff() messages.TariffType {
	return messages.TariffType{
		Currency: "EUR",
		TariffID: "tariff-1",
	}
}

func testFirmware() messages.FirmwareType {
	return messages.FirmwareType{
		Location:         "https://example.com/firmware.bin",
		RetrieveDateTime: testTime(),
	}
}

func requireCPRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example")
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

func requireCSMSRejectsWrongDirection[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
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

func TestBatterySwap21_RequestValidation(t *testing.T) {
	validator := must21Validator(t, "BatterySwap", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.BatterySwapRequest{
				BatteryData: []messages.BatteryDataType{testBatteryData()},
				EventType:   "BatteryIn",
				IDToken:     testIDToken(),
				RequestID:   1,
			},
			Valid: true,
		},
		{
			Name: "missing eventType",
			Message: map[string]any{
				"batteryData": []map[string]any{
					{
						"evseId":       1,
						"serialNumber": "battery-1",
						"soC":          80,
						"soH":          90,
					},
				},
				"idToken": map[string]any{
					"idToken": "id-token-1",
					"type":    "ISO14443",
				},
				"requestId": 1,
			},
			Valid: false,
		},
		{
			Name: "exceeds maxLength serialNumber",
			Message: messages.BatterySwapRequest{
				BatteryData: []messages.BatteryDataType{
					{
						EVSEID:       1,
						SerialNumber: longString(51),
						SoC:          decimal.NewFromInt(80),
						SoH:          decimal.NewFromInt(90),
					},
				},
				EventType: "BatteryIn",
				IDToken:   testIDToken(),
				RequestID: 1,
			},
			Valid: false,
		},
		{
			Name: "invalid enum eventType",
			Message: messages.BatterySwapRequest{
				BatteryData: []messages.BatteryDataType{testBatteryData()},
				EventType:   "InvalidBatterySwapEvent",
				IDToken:     testIDToken(),
				RequestID:   1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBatterySwap21_ResponseValidation(t *testing.T) {
	validator := must21Validator(t, "BatterySwap", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid",
			Message: messages.BatterySwapResponse{},
			Valid:   true,
		},
		{
			Name: "exceeds maxLength customData.vendorId",
			Message: messages.BatterySwapResponse{
				CustomData: invalidCustomData(),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestBatterySwap21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection(t, v21profiles.BatterySwap)
}
