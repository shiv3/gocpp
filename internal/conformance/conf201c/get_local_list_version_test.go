package conf201c

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
	v201 "github.com/shiv3/gocpp/v201"
	"github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

const (
	version201     = "2.0.1"
	subprotocol201 = "ocpp2.0.1"
)

func registry201(t *testing.T) *schema.Registry {
	t.Helper()

	reg := conformance.SchemaRegistry(v201.RegisterSchemas)
	return reg
}

func validator201(t *testing.T, action, kind string) *schema.Validator {
	t.Helper()
	return conformance.MustValidator(t, registry201(t), version201, action, kind)
}

func int32Ptr(v int32) *int32 {
	return &v
}

func boolPtr(v bool) *bool {
	return &v
}

func strPtr(v string) *string {
	return &v
}

func fixedTime201() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func component201() messages.ComponentType {
	return messages.ComponentType{
		Name:     "component1",
		Instance: strPtr("instance1"),
		EVSE: &messages.EVSEType{
			ID:          2,
			ConnectorID: int32Ptr(2),
		},
	}
}

func variable201() messages.VariableType {
	return messages.VariableType{
		Name:     "variable1",
		Instance: strPtr("instance1"),
	}
}

func componentVariable201() messages.ComponentVariableType {
	return messages.ComponentVariableType{
		Component: component201(),
		Variable:  &messages.VariableType{Name: "variable1", Instance: strPtr("instance1")},
	}
}

func requireCSMSHandlerInvalidDirection201[Req, Resp any](t *testing.T, msg ocppj.Message[Req, Resp]) {
	t.Helper()

	srv := csms.NewServer(csms.WithSubProtocols(subprotocol201))
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

func TestGetLocalListVersion201_RequestValidation(t *testing.T) {
	validator := validator201(t, "GetLocalListVersion", "request")

	cases := []conformance.ValidationCase{
		{
			Name:    "valid empty request",
			Message: messages.GetLocalListVersionRequest{},
			Valid:   true,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLocalListVersion201_ResponseValidation(t *testing.T) {
	validator := validator201(t, "GetLocalListVersion", "response")

	cases := []conformance.ValidationCase{
		{
			Name: "valid positive version number",
			Message: messages.GetLocalListVersionResponse{
				VersionNumber: 1,
			},
			Valid: true,
		},
		{
			Name: "valid zero version number",
			Message: messages.GetLocalListVersionResponse{
				VersionNumber: 0,
			},
			Valid: true,
		},
		{
			Name:    "valid zero-value response",
			Message: messages.GetLocalListVersionResponse{},
			Valid:   true,
		},
		{
			Name:    "invalid missing versionNumber",
			Message: map[string]any{},
			Valid:   false,
		},
		{
			Name: "invalid versionNumber below minimum",
			Message: map[string]any{
				"versionNumber": -1,
			},
			Valid: false,
		},
	}

	conformance.RunValidationTable(t, validator, cases)
}

func TestGetLocalListVersion201_Direction(t *testing.T) {
	requireCPHandlerInvalidDirection201(t, v201profiles.GetLocalListVersion)
}
