package conf201d

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
	"github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

const subprotocol201 = "ocpp2.0.1"

func init() {
	decimal.MarshalJSONWithoutQuotes = true
}

func ptr[T any](v T) *T {
	return &v
}

func fixedTime201() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func longString(n int) string {
	return strings.Repeat("x", n)
}

func dec(v string) decimal.Decimal {
	d, err := decimal.NewFromString(v)
	if err != nil {
		panic(err)
	}
	return d
}

func statusInfo201(reasonCode string) *messages.StatusInfoType {
	return &messages.StatusInfoType{ReasonCode: reasonCode, AdditionalInfo: ptr("someInfo")}
}

func component201() messages.ComponentType {
	return messages.ComponentType{
		Name:     "component1",
		Instance: ptr("instance1"),
		EVSE: &messages.EVSEType{
			ID:          2,
			ConnectorID: ptr(int32(2)),
		},
	}
}

func variable201() messages.VariableType {
	return messages.VariableType{
		Name:     "variable1",
		Instance: ptr("instance1"),
	}
}

func messageContent201() messages.MessageContentType {
	return messages.MessageContentType{
		Format:  "UTF8",
		Content: "hello world",
	}
}

func messageInfo201() messages.MessageInfoType {
	return messages.MessageInfoType{
		ID:            42,
		Priority:      "AlwaysFront",
		State:         ptr("Idle"),
		StartDateTime: ptr(fixedTime201()),
		Message:       messageContent201(),
	}
}

func chargingSchedulePeriod201() messages.ChargingSchedulePeriodType {
	return messages.ChargingSchedulePeriodType{
		StartPeriod: 0,
		Limit:       dec("10.0"),
	}
}

func chargingSchedule201() messages.ChargingScheduleType {
	return messages.ChargingScheduleType{
		ID:                     1,
		StartSchedule:          ptr(fixedTime201()),
		Duration:               ptr(int32(600)),
		ChargingRateUnit:       "W",
		MinChargingRate:        ptr(dec("6.0")),
		ChargingSchedulePeriod: []messages.ChargingSchedulePeriodType{chargingSchedulePeriod201()},
	}
}

func chargingProfile201(purpose string) messages.ChargingProfileType {
	return messages.ChargingProfileType{
		ID:                     1,
		StackLevel:             0,
		ChargingProfilePurpose: purpose,
		ChargingProfileKind:    "Absolute",
		ChargingSchedule:       []messages.ChargingScheduleType{chargingSchedule201()},
	}
}

func idToken201(tokenType string) messages.IdTokenType {
	return messages.IdTokenType{
		IDToken: "1234",
		Type:    tokenType,
	}
}

func requireCSMSHandlerInvalidDirection201[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(subprotocol201))
	defer srv.Close()
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
		var resp Resp
		return resp, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func requireCPHandlerInvalidDirection201[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	client := cp.NewClient("CP_1", "ws://example.invalid", cp.WithSubProtocols(subprotocol201))
	wrongDirection := ocppj.Message[Req, Resp]{
		Action:    msg.Action,
		Direction: ocppj.SentByCP,
	}

	err := cp.On(client, wrongDirection, func(ctx context.Context, req Req) (Resp, error) {
		var resp Resp
		return resp, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}

func skipSchemaOverride201(t *testing.T, name string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		t.Skip("numeric or array-min bound is not present in the bundled OCA schema")
	})
}

func TestNotifyCustomerInformation201_RequestValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyCustomerInformation", "request")

	cases := []conformance.ValidationCase{
		{
			Name: "valid full request with tbc false",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        "dummyData",
				Tbc:         ptr(false),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				RequestID:   42,
			},
			Valid: true,
		},
		{
			Name: "valid full request with tbc true",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        "dummyData",
				Tbc:         ptr(true),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				RequestID:   42,
			},
			Valid: true,
		},
		{
			Name: "valid without tbc",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        "dummyData",
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				RequestID:   42,
			},
			Valid: true,
		},
		{
			Name: "valid zero seqNo",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        "dummyData",
				GeneratedAt: fixedTime201(),
				RequestID:   42,
			},
			Valid: true,
		},
		{
			Name: "valid zero requestId",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        "dummyData",
				GeneratedAt: fixedTime201(),
			},
			Valid: true,
		},
		{
			Name: "valid zero generatedAt seqNo and requestId",
			Message: messages.NotifyCustomerInformationRequest{
				Data: "dummyData",
			},
			Valid: true,
		},
		{
			Name: "invalid missing data",
			Message: map[string]any{
				"seqNo":       0,
				"generatedAt": fixedTime201(),
				"requestId":   42,
			},
			Valid: false,
		},
		{
			Name: "invalid data exceeds maxLength 512",
			Message: messages.NotifyCustomerInformationRequest{
				Data:        longString(513),
				Tbc:         ptr(false),
				SeqNo:       0,
				GeneratedAt: fixedTime201(),
				RequestID:   42,
			},
			Valid: false,
		},
		{
			Name: "invalid seqNo below minimum",
			Message: map[string]any{
				"data":        "dummyData",
				"seqNo":       -1,
				"generatedAt": fixedTime201(),
				"requestId":   42,
			},
			Valid: false,
		},
		{
			Name: "invalid requestId below minimum",
			Message: map[string]any{
				"data":        "dummyData",
				"seqNo":       0,
				"generatedAt": fixedTime201(),
				"requestId":   -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyCustomerInformation201_ResponseValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	validator := conformance.MustValidator(t, reg, "2.0.1", "NotifyCustomerInformation", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.NotifyCustomerInformationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestNotifyCustomerInformation201_Direction(t *testing.T) {
	requireCSMSHandlerInvalidDirection201(t, v201profiles.NotifyCustomerInformation)
}
