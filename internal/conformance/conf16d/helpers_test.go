package conf16d_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func must16Validator(t *testing.T, action, kind string) *schema.Validator {
	t.Helper()

	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	return conformance.MustValidator(t, reg, "1.6", action, kind)
}

func int32Ptr(i int32) *int32 {
	return &i
}

func timePtr(t time.Time) *time.Time {
	return &t
}

func decimalPtr(d decimal.Decimal) *decimal.Decimal {
	return &d
}

func testTime() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func testChargingSchedule() messages.ChargingSchedule {
	return messages.ChargingSchedule{
		ChargingRateUnit: messages.ChargingScheduleChargingRateUnitW,
		ChargingSchedulePeriod: []messages.ChargingSchedulePeriod{
			{
				Limit:       decimal.NewFromInt(10),
				StartPeriod: 0,
			},
		},
	}
}

func fullChargingSchedule() messages.ChargingSchedule {
	return messages.ChargingSchedule{
		ChargingRateUnit: messages.ChargingScheduleChargingRateUnitW,
		ChargingSchedulePeriod: []messages.ChargingSchedulePeriod{
			{
				Limit:        decimal.NewFromInt(10),
				NumberPhases: int32Ptr(3),
				StartPeriod:  0,
			},
		},
		Duration:        int32Ptr(600),
		MinChargingRate: decimalPtr(decimal.NewFromInt(1)),
		StartSchedule:   timePtr(testTime()),
	}
}

func testCsChargingProfiles() messages.CsChargingProfiles {
	return messages.CsChargingProfiles{
		ChargingProfileID:      1,
		ChargingProfileKind:    messages.CsChargingProfilesChargingProfileKindAbsolute,
		ChargingProfilePurpose: messages.CsChargingProfilesChargingProfilePurposeChargePointMaxProfile,
		ChargingSchedule:       testChargingSchedule(),
		StackLevel:             1,
	}
}

func fullCsChargingProfiles() messages.CsChargingProfiles {
	recurrency := messages.CsChargingProfilesRecurrencyKindDaily
	return messages.CsChargingProfiles{
		ChargingProfileID:      1,
		ChargingProfileKind:    messages.CsChargingProfilesChargingProfileKindRecurring,
		ChargingProfilePurpose: messages.CsChargingProfilesChargingProfilePurposeChargePointMaxProfile,
		ChargingSchedule:       fullChargingSchedule(),
		RecurrencyKind:         &recurrency,
		StackLevel:             1,
		TransactionID:          int32Ptr(42),
		ValidFrom:              timePtr(testTime()),
		ValidTo:                timePtr(testTime().Add(time.Hour)),
	}
}

func testCertificateHashData() messages.CertificateHashDataType {
	return messages.CertificateHashDataType{
		HashAlgorithm:  "SHA256",
		IssuerKeyHash:  "hash01",
		IssuerNameHash: "hash00",
		SerialNumber:   "serial0",
	}
}

func testFirmware() messages.FirmwareType {
	return messages.FirmwareType{
		InstallDateTime:    timePtr(testTime()),
		Location:           "https://someurl",
		RetrieveDateTime:   testTime(),
		Signature:          "deadc0de",
		SigningCertificate: "1337c0de",
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

	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
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
