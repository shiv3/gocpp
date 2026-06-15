package conf21c

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	ocppj "github.com/shiv3/gocpp/core/ocppj"
	schema "github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v21 "github.com/shiv3/gocpp/v21"
	messages "github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const subprotocol21 = "ocpp2.1"

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func ptr[T any](v T) *T {
	return &v
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func fixedTime21() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func dec21(v string) decimal.Decimal {
	d, err := decimal.NewFromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

func idToken21() messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: "token-1",
		Type:    "Central",
	}
}

func statusInfo21(reasonCode string) *messages.StatusInfoType {
	return &messages.StatusInfoType{
		AdditionalInfo: ptr("details"),
		ReasonCode:     reasonCode,
	}
}

func component21() messages.ComponentType {
	return messages.ComponentType{
		EVSE: &messages.EVSEType{
			ConnectorID: ptr(int32(1)),
			ID:          1,
		},
		Instance: ptr("instance-1"),
		Name:     "component",
	}
}

func variable21() messages.VariableType {
	return messages.VariableType{
		Instance: ptr("instance-1"),
		Name:     "variable",
	}
}

func certificateHashData21() messages.CertificateHashDataType {
	return messages.CertificateHashDataType{
		HashAlgorithm:  "SHA256",
		IssuerKeyHash:  "issuer-key-hash",
		IssuerNameHash: "issuer-name-hash",
		SerialNumber:   "serial-1",
	}
}

func certificateHashDataChain21() messages.CertificateHashDataChainType {
	return messages.CertificateHashDataChainType{
		CertificateHashData: certificateHashData21(),
		CertificateType:     "CSMSRootCertificate",
	}
}

func chargingSchedulePeriod21() messages.ChargingSchedulePeriodType {
	return messages.ChargingSchedulePeriodType{
		Limit:       ptr(dec21("10.0")),
		StartPeriod: 0,
	}
}

func chargingSchedule21() messages.ChargingScheduleType {
	return messages.ChargingScheduleType{
		ChargingRateUnit:       "W",
		ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{chargingSchedulePeriod21()},
		ID:                     1,
	}
}

func derCurve21() messages.DERCurveType {
	return messages.DERCurveType{
		CurveData: []messages.DERCurvePointsType{
			{
				X: dec21("1.0"),
				Y: dec21("2.0"),
			},
		},
		Hysteresis: &messages.HysteresisType{
			HysteresisHigh: ptr(dec21("1.0")),
			HysteresisLow:  ptr(dec21("0.5")),
		},
		Priority: 1,
		ReactivePowerParams: &messages.ReactivePowerParamsType{
			AutonomousVRefEnable: ptr(true),
			VRef:                 ptr(dec21("230.0")),
		},
		VoltageParams: &messages.VoltageParamsType{
			PowerDuringCessation: ptr("Active"),
		},
		YUnit: "PctMaxW",
	}
}

func setDERControlRequest21() messages.SetDERControlRequest {
	return messages.SetDERControlRequest{
		ControlID:   "control-1",
		ControlType: "VoltVar",
		Curve:       ptr(derCurve21()),
		EnterService: &messages.EnterServiceType{
			HighFreq:    dec21("60.5"),
			HighVoltage: dec21("240.0"),
			LowFreq:     dec21("59.5"),
			LowVoltage:  dec21("208.0"),
			Priority:    1,
		},
		FixedPFAbsorb: &messages.FixedPFType{
			Displacement: dec21("0.95"),
			Excitation:   true,
			Priority:     1,
		},
		FixedPFInject: &messages.FixedPFType{
			Displacement: dec21("0.90"),
			Excitation:   false,
			Priority:     2,
		},
		FixedVar: &messages.FixedVarType{
			Priority: 1,
			Setpoint: dec21("5.0"),
			Unit:     "PctMaxVar",
		},
		FreqDroop: &messages.FreqDroopType{
			OverDroop:    dec21("0.1"),
			OverFreq:     dec21("60.5"),
			Priority:     1,
			ResponseTime: dec21("1.0"),
			UnderDroop:   dec21("0.1"),
			UnderFreq:    dec21("59.5"),
		},
		Gradient: &messages.GradientType{
			Gradient:     dec21("1.0"),
			Priority:     1,
			SoftGradient: dec21("2.0"),
		},
		IsDefault: false,
		LimitMaxDischarge: &messages.LimitMaxDischargeType{
			PctMaxDischargePower:    ptr(dec21("80.0")),
			PowerMonitoringMustTrip: ptr(derCurve21()),
			Priority:                1,
		},
	}
}

func requireCSMSRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(subprotocol21))
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

func requireCPRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(subprotocol21))
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

func TestAuthorize21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "Authorize", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.AuthorizeRequest{
				IDToken: idToken21(),
			},
			Valid: true,
		},
		{
			Name:    "missing idToken",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.AuthorizeRequest{
				Certificate: ptr(longString(10001)),
				IDToken:     idToken21(),
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.AuthorizeRequest{
				IDToken: idToken21(),
				Iso15118CertificateHashData: []messages.OCSPRequestDataType{
					{
						HashAlgorithm:  "BogusHash",
						IssuerKeyHash:  "issuer-key-hash",
						IssuerNameHash: "issuer-name-hash",
						ResponderURL:   "https://example.invalid/ocsp",
						SerialNumber:   "serial-1",
					},
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestAuthorize21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "Authorize", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{
					Status: "Accepted",
				},
			},
			Valid: true,
		},
		{
			Name:    "missing idTokenInfo",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "exceeds maxLength",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{
					Language1: ptr(longString(9)),
					Status:    "Accepted",
				},
			},
			Valid: false,
		},
		{
			Name: "invalid enum",
			Message: messages.AuthorizeResponse{
				IDTokenInfo: messages.IdTokenInfoType{
					Status: "BogusStatus",
				},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestAuthorize21_Direction(t *testing.T) {
	requireCSMSRejectsWrongDirection21(t, v21profiles.Authorize)
}
