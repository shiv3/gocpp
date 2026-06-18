package conf16b

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/internal/conformance"
	v16 "github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	v16profiles "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestStatusNotification16_RequestValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "StatusNotification", "request")

	now := fixedTime16()
	cases := []conformance.ValidationCase{
		{
			Name: "valid full request",
			Message: messages.StatusNotificationRequest{
				ConnectorID:     0,
				ErrorCode:       messages.StatusNotificationRequestErrorCodeNoError,
				Info:            ptr("mockInfo"),
				Status:          messages.StatusNotificationRequestStatusAvailable,
				Timestamp:       &now,
				VendorID:        ptr("mockId"),
				VendorErrorCode: ptr("mockErrorCode"),
			},
			Valid: true,
		},
		{
			Name: "valid minimal request",
			Message: messages.StatusNotificationRequest{
				ConnectorID: 0,
				ErrorCode:   messages.StatusNotificationRequestErrorCodeNoError,
				Status:      messages.StatusNotificationRequestStatusAvailable,
			},
			Valid: true,
		},
		{
			Name: "invalid missing connectorId",
			Message: map[string]any{
				"errorCode": messages.StatusNotificationRequestErrorCodeNoError,
				"status":    messages.StatusNotificationRequestStatusAvailable,
			},
			Valid: false,
		},
		{
			Name: "invalid connectorId below minimum",
			Message: map[string]any{
				"connectorId": -1,
				"errorCode":   messages.StatusNotificationRequestErrorCodeNoError,
				"status":      messages.StatusNotificationRequestStatusAvailable,
			},
			Valid: false,
		},
		{
			Name: "invalid missing errorCode",
			Message: map[string]any{
				"connectorId": int32(0),
				"status":      messages.StatusNotificationRequestStatusAvailable,
			},
			Valid: false,
		},
		{
			Name: "invalid missing status",
			Message: map[string]any{
				"connectorId": int32(0),
				"errorCode":   messages.StatusNotificationRequestErrorCodeNoError,
			},
			Valid: false,
		},
		{
			Name: "invalid errorCode enum",
			Message: messages.StatusNotificationRequest{
				ConnectorID: 0,
				ErrorCode:   messages.StatusNotificationRequestErrorCode("invalidErrorCode"),
				Status:      messages.StatusNotificationRequestStatusAvailable,
			},
			Valid: false,
		},
		{
			Name: "invalid status enum",
			Message: messages.StatusNotificationRequest{
				ConnectorID: 0,
				ErrorCode:   messages.StatusNotificationRequestErrorCodeNoError,
				Status:      messages.StatusNotificationRequestStatus("invalidChargePointStatus"),
			},
			Valid: false,
		},
		{
			Name: "invalid info exceeds maxLength 50",
			Message: messages.StatusNotificationRequest{
				ConnectorID: 0,
				ErrorCode:   messages.StatusNotificationRequestErrorCodeNoError,
				Info:        ptr(strings.Repeat("x", 51)),
				Status:      messages.StatusNotificationRequestStatusAvailable,
			},
			Valid: false,
		},
		{
			Name: "invalid vendorErrorCode exceeds maxLength 50",
			Message: messages.StatusNotificationRequest{
				ConnectorID:     0,
				ErrorCode:       messages.StatusNotificationRequestErrorCodeNoError,
				Status:          messages.StatusNotificationRequestStatusAvailable,
				VendorErrorCode: ptr(strings.Repeat("x", 51)),
			},
			Valid: false,
		},
		{
			Name: "invalid vendorId exceeds maxLength 255",
			Message: messages.StatusNotificationRequest{
				ConnectorID: 0,
				ErrorCode:   messages.StatusNotificationRequestErrorCodeNoError,
				Status:      messages.StatusNotificationRequestStatusAvailable,
				VendorID:    ptr(strings.Repeat("x", 256)),
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification16_ResponseValidation(t *testing.T) {
	reg := conformance.SchemaRegistry(v16.RegisterSchemas)
	validator := conformance.MustValidator(t, reg, "1.6", "StatusNotification", "response")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty response",
			Message: messages.StatusNotificationResponse{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestStatusNotification16_Direction(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	wrongDirection := ocppj.Message[messages.StatusNotificationRequest, messages.StatusNotificationResponse]{
		Action:    v16profiles.StatusNotification.Action,
		Direction: ocppj.SentByCSMS,
	}

	err := csms.On(srv, wrongDirection, func(ctx context.Context, c *csms.Conn, req messages.StatusNotificationRequest) (messages.StatusNotificationResponse, error) {
		return messages.StatusNotificationResponse{}, nil
	})

	require.True(t, errors.Is(err, ocppj.ErrInvalidDirection))
}
