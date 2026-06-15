package conf21g

import (
	"context"
	"encoding/json"
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
	"github.com/shiv3/gocpp/v21/messages"
	v21profiles "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func strPtr21(s string) *string {
	return &s
}

func int32Ptr21(i int32) *int32 {
	return &i
}

func boolPtr21(b bool) *bool {
	return &b
}

func testTime21() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

const timeFormatRFC3339Nano21 = time.RFC3339Nano
const timeHour21 = time.Hour

func dec21(i int64) decimal.Decimal {
	return decimal.NewFromInt(i)
}

func customData21() *messages.CustomDataType {
	return &messages.CustomDataType{VendorID: "vendor"}
}

func customDataMap21() map[string]any {
	return map[string]any{"vendorId": "vendor"}
}

func statusInfo21() *messages.StatusInfoType {
	return &messages.StatusInfoType{
		AdditionalInfo: strPtr21("details"),
		CustomData:     customData21(),
		ReasonCode:     "OK",
	}
}

func statusInfoMap21() map[string]any {
	return map[string]any{
		"additionalInfo": "details",
		"customData":     customDataMap21(),
		"reasonCode":     "OK",
	}
}

func certificateHashData21() messages.CertificateHashDataType {
	return messages.CertificateHashDataType{
		CustomData:     customData21(),
		HashAlgorithm:  "SHA256",
		IssuerKeyHash:  "issuer-key-hash",
		IssuerNameHash: "issuer-name-hash",
		SerialNumber:   "serial",
	}
}

func evse21() *messages.EVSEType {
	return &messages.EVSEType{
		ConnectorID: int32Ptr21(1),
		CustomData:  customData21(),
		ID:          1,
	}
}

func messageContent21(format, content string) messages.MessageContentType {
	return messages.MessageContentType{
		Content:    content,
		CustomData: customData21(),
		Format:     format,
		Language:   strPtr21("en"),
	}
}

func messageInfo21() messages.MessageInfoType {
	return messages.MessageInfoType{
		CustomData: customData21(),
		Display: &messages.ComponentType{
			CustomData: customData21(),
			EVSE:       evse21(),
			Instance:   strPtr21("main"),
			Name:       "Display",
		},
		EndDateTime:   &[]time.Time{testTime21().Add(time.Hour)}[0],
		ID:            1,
		Message:       messageContent21("UTF8", "Ready"),
		MessageExtra:  []messages.MessageContentType{messageContent21("ASCII", "Go")},
		Priority:      "AlwaysFront",
		StartDateTime: &[]time.Time{testTime21()}[0],
		State:         strPtr21("Charging"),
		TransactionID: strPtr21("transaction-1"),
	}
}

func address21() *messages.AddressType {
	return &messages.AddressType{
		Address1:   "Main Street 1",
		Address2:   strPtr21("Suite 1"),
		City:       "Amsterdam",
		Country:    "Netherlands",
		CustomData: customData21(),
		Name:       "Example BV",
		PostalCode: strPtr21("1000AA"),
	}
}

func derCurve21() messages.DERCurveType {
	return messages.DERCurveType{
		CurveData: []messages.DERCurvePointsType{
			{
				CustomData: customData21(),
				X:          dec21(1),
				Y:          dec21(2),
			},
		},
		CustomData: customData21(),
		Priority:   1,
		VoltageParams: &messages.VoltageParamsType{
			CustomData:           customData21(),
			PowerDuringCessation: strPtr21("Active"),
		},
		YUnit: "PctMaxW",
	}
}

func derCurveGet21() messages.DERCurveGetType {
	return messages.DERCurveGetType{
		CustomData:   customData21(),
		Curve:        derCurve21(),
		CurveType:    "VoltWatt",
		ID:           "curve-1",
		IsDefault:    true,
		IsSuperseded: false,
	}
}

func enterService21() messages.EnterServiceGetType {
	return messages.EnterServiceGetType{
		CustomData: customData21(),
		EnterService: messages.EnterServiceType{
			CustomData:  customData21(),
			HighFreq:    dec21(61),
			HighVoltage: dec21(240),
			LowFreq:     dec21(59),
			LowVoltage:  dec21(220),
			Priority:    1,
		},
		ID: "enter-service-1",
	}
}

func fixedPF21() messages.FixedPFGetType {
	return messages.FixedPFGetType{
		CustomData: customData21(),
		FixedPF: messages.FixedPFType{
			CustomData:   customData21(),
			Displacement: dec21(1),
			Excitation:   true,
			Priority:     1,
		},
		ID:           "fixed-pf-1",
		IsDefault:    true,
		IsSuperseded: false,
	}
}

func fixedVar21() messages.FixedVarGetType {
	return messages.FixedVarGetType{
		CustomData: customData21(),
		FixedVar: messages.FixedVarType{
			CustomData: customData21(),
			Priority:   1,
			Setpoint:   dec21(10),
			Unit:       "PctMaxVar",
		},
		ID:           "fixed-var-1",
		IsDefault:    true,
		IsSuperseded: false,
	}
}

func freqDroop21() messages.FreqDroopGetType {
	return messages.FreqDroopGetType{
		CustomData: customData21(),
		FreqDroop: messages.FreqDroopType{
			CustomData:   customData21(),
			OverDroop:    dec21(1),
			OverFreq:     dec21(61),
			Priority:     1,
			ResponseTime: dec21(2),
			UnderDroop:   dec21(1),
			UnderFreq:    dec21(59),
		},
		ID:           "freq-droop-1",
		IsDefault:    true,
		IsSuperseded: false,
	}
}

func gradient21() messages.GradientGetType {
	return messages.GradientGetType{
		CustomData: customData21(),
		Gradient: messages.GradientType{
			CustomData:   customData21(),
			Gradient:     dec21(1),
			Priority:     1,
			SoftGradient: dec21(2),
		},
		ID: "gradient-1",
	}
}

func limitMaxDischarge21() messages.LimitMaxDischargeGetType {
	return messages.LimitMaxDischargeGetType{
		CustomData: customData21(),
		ID:         "limit-max-discharge-1",
		IsDefault:  true,
		LimitMaxDischarge: messages.LimitMaxDischargeType{
			CustomData: customData21(),
			Priority:   1,
		},
		IsSuperseded: false,
	}
}

func cloneMap21(src map[string]any) map[string]any {
	raw, err := json.Marshal(src)
	if err != nil {
		panic(err)
	}
	var dst map[string]any
	if err := json.Unmarshal(raw, &dst); err != nil {
		panic(err)
	}
	return dst
}

func without21(src map[string]any, path ...any) map[string]any {
	dst := cloneMap21(src)
	parent := any(dst)
	for _, step := range path[:len(path)-1] {
		parent = step21(parent, step)
	}
	last := path[len(path)-1].(string)
	delete(parent.(map[string]any), last)
	return dst
}

func with21(src map[string]any, value any, path ...any) map[string]any {
	dst := cloneMap21(src)
	parent := any(dst)
	for _, step := range path[:len(path)-1] {
		parent = step21(parent, step)
	}
	parent.(map[string]any)[path[len(path)-1].(string)] = value
	return dst
}

func step21(parent any, step any) any {
	switch p := parent.(type) {
	case map[string]any:
		return p[step.(string)]
	case []any:
		return p[step.(int)]
	default:
		panic("unsupported path")
	}
}

func requireCPRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
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

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func requireCSMSRejectsWrongDirection21[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
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

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func TestCertificateSigned21_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "CertificateSigned", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid request",
			Message: messages.CertificateSignedRequest{
				CertificateChain: "sampleCert",
				CertificateType:  strPtr21("ChargingStationCertificate"),
				CustomData:       customData21(),
				RequestID:        int32Ptr21(1),
			},
			Valid: true,
		},
		{
			Name:    "missing certificateChain",
			Message: map[string]any{"certificateType": "ChargingStationCertificate", "customData": customDataMap21(), "requestId": 1},
			Valid:   false,
		},
		{
			Name: "certificateChain exceeds maxLength",
			Message: messages.CertificateSignedRequest{
				CertificateChain: strings.Repeat("x", 10001),
			},
			Valid: false,
		},
		{
			Name: "invalid certificateType enum",
			Message: messages.CertificateSignedRequest{
				CertificateChain: "sampleCert",
				CertificateType:  strPtr21("InvalidCertificateType"),
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"certificateChain": "sampleCert",
				"customData":       map[string]any{},
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"certificateChain": "sampleCert",
				"customData":       map[string]any{"vendorId": strings.Repeat("x", 256)},
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCertificateSigned21_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v21.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.1", "CertificateSigned", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid response",
			Message: messages.CertificateSignedResponse{
				CustomData: customData21(),
				Status:     "Accepted",
				StatusInfo: statusInfo21(),
			},
			Valid: true,
		},
		{
			Name:    "missing status",
			Message: map[string]any{"customData": customDataMap21(), "statusInfo": statusInfoMap21()},
			Valid:   false,
		},
		{
			Name: "invalid status enum",
			Message: messages.CertificateSignedResponse{
				Status: "InvalidStatus",
			},
			Valid: false,
		},
		{
			Name: "missing statusInfo.reasonCode",
			Message: map[string]any{
				"status":     "Accepted",
				"statusInfo": map[string]any{"additionalInfo": "details", "customData": customDataMap21()},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.reasonCode exceeds maxLength",
			Message: messages.CertificateSignedResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					ReasonCode: strings.Repeat("x", 21),
				},
			},
			Valid: false,
		},
		{
			Name: "statusInfo.additionalInfo exceeds maxLength",
			Message: messages.CertificateSignedResponse{
				Status: "Accepted",
				StatusInfo: &messages.StatusInfoType{
					AdditionalInfo: strPtr21(strings.Repeat("x", 1025)),
					ReasonCode:     "OK",
				},
			},
			Valid: false,
		},
		{
			Name: "missing customData.vendorId",
			Message: map[string]any{
				"customData": map[string]any{},
				"status":     "Accepted",
			},
			Valid: false,
		},
		{
			Name: "customData.vendorId exceeds maxLength",
			Message: map[string]any{
				"customData": map[string]any{"vendorId": strings.Repeat("x", 256)},
				"status":     "Accepted",
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestCertificateSigned21_Direction(t *testing.T) {
	requireCPRejectsWrongDirection21(t, v21profiles.CertificateSigned)
}
