package e2e

import (
	"context"
	"github.com/shiv3/gocpp/internal/splitmode"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v201"
	"github.com/stretchr/testify/require"
)

type anyReq = map[string]any

func anyMsg(action string) ocppj.Message[anyReq, anyReq] {
	return ocppj.Message[anyReq, anyReq]{Action: action, Direction: ocppj.SentByCP}
}

// TestE2E_SchemaViolationErrorCodes checks that a schema rejection of an inbound
// CALL is answered with the CALLERROR code matching the violated JSON-Schema
// keyword (OCPP 1.6J §4.2.3, OCPP 2.0.1 Part 4 §4.2.3), in strict and lenient
// modes, and that a payload that is not an object stays FormationViolation.
func TestE2E_SchemaViolationErrorCodes(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	require.NoError(t, v201.RegisterSchemas(reg))

	type tc struct {
		name    string
		sub     string
		mode    csms.Option
		action  string
		payload string
		want    string
	}
	strict := csms.WithStrictSchema(true)
	lenient := csms.WithLenientSchema()
	cases := []tc{
		{"1.6 missing idTag", "ocpp1.6", strict, "Authorize", `{}`, "OccurenceConstraintViolation"},
		{"1.6 missing idTag lenient", "ocpp1.6", lenient, "Authorize", `{}`, "OccurenceConstraintViolation"},
		{"2.0.1 missing idToken", "ocpp2.0.1", strict, "Authorize", `{}`, "OccurrenceConstraintViolation"},
		{"2.0.1 missing idToken lenient", "ocpp2.0.1", lenient, "Authorize", `{}`, "OccurrenceConstraintViolation"},
		{"1.6 idTag too long strict", "ocpp1.6", strict, "Authorize", `{"idTag":"012345678901234567890"}`, "PropertyConstraintViolation"},
		{"1.6 unknown enum strict", "ocpp1.6", strict, "StatusNotification",
			`{"connectorId":1,"errorCode":"Nope","status":"Available"}`, "PropertyConstraintViolation"},
		{"1.6 connectorId string strict", "ocpp1.6", strict, "StartTransaction",
			`{"connectorId":"one","idTag":"x","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`, "TypeConstraintViolation"},
		{"1.6 connectorId string lenient", "ocpp1.6", lenient, "StartTransaction",
			`{"connectorId":"one","idTag":"x","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`, "TypeConstraintViolation"},
		{"1.6 payload not an object", "ocpp1.6", strict, "Authorize", `[]`, "FormationViolation"},
		{"1.6 payload not an object lenient", "ocpp1.6", lenient, "Authorize", `[]`, "FormationViolation"},
		{"2.0.1 payload not an object", "ocpp2.0.1", strict, "Authorize", `[]`, "FormatViolation"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			srv := splitmode.NewServer(csms.WithSubProtocols(c.sub), csms.WithSchemaRegistry(reg), c.mode)
			require.NoError(t, csms.On(srv, anyMsg(c.action), func(ctx context.Context, _ *csms.Conn, req anyReq) (anyReq, error) {
				t.Error("handler reached for an invalid request")
				return anyReq{}, nil
			}))
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()
			client := cp.NewClient("CP_1", "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", cp.WithSubProtocols(c.sub))
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			require.NoError(t, client.Connect(ctx))
			defer client.Close()

			_, err := cp.CallRaw(ctx, client, c.action, []byte(c.payload))
			var ce *ocppj.CallError
			require.ErrorAs(t, err, &ce)
			require.Equal(t, c.want, string(ce.Code))
		})
	}
}

// TestE2E_TypedDecodeErrorCode checks the typed-handler decode failure mapping
// without any schema validation: a wrong JSON type is TypeConstraintViolation,
// a payload that is not an object stays FormationViolation.
func TestE2E_TypedDecodeErrorCode(t *testing.T) {
	type req struct {
		ConnectorID int `json:"connectorId"`
	}
	msg := ocppj.Message[req, anyReq]{Action: "Probe", Direction: ocppj.SentByCP}
	srv := splitmode.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, csms.On(srv, msg, func(ctx context.Context, _ *csms.Conn, r req) (anyReq, error) {
		return anyReq{}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	client := cp.NewClient("CP_1", "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	for payload, want := range map[string]string{
		`{"connectorId":"one"}`: "TypeConstraintViolation",
		`[]`:                    "FormationViolation",
	} {
		_, err := cp.CallRaw(ctx, client, "Probe", []byte(payload))
		var ce *ocppj.CallError
		require.ErrorAs(t, err, &ce)
		require.Equal(t, want, string(ce.Code), payload)
	}
}
