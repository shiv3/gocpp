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
	"github.com/stretchr/testify/require"
)

// outboundValidationRig runs a CSMS with the given schema options against a CP
// that does no validation itself, so what reaches the wire is observable.
func outboundValidationRig(t *testing.T, opts ...csms.Option) (*csms.Server, *cp.Client, context.Context) {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	srv := splitmode.NewServer(append([]csms.Option{
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithSchemaRegistry(reg),
	}, opts...)...)
	// The handler answers with an empty (schema-invalid) response: status is required.
	require.NoError(t, csms.On(srv, bootMsg, func(context.Context, *csms.Conn, bootReq) (bootResp, error) {
		return bootResp{}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	client := cp.NewClient("CP_OV", "ws"+ts.URL[len("http"):]+"/ocpp/CP_OV", cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	require.NoError(t, client.Connect(ctx))
	t.Cleanup(client.Close)
	return srv, client, ctx
}

func TestOutboundValidation_HandlerReply(t *testing.T) {
	for _, mode := range []struct {
		name string
		opt  csms.Option
	}{{"strict", csms.WithStrictSchema(true)}, {"lenient", csms.WithLenientSchema()}} {
		t.Run(mode.name+"/default rejects the invalid reply", func(t *testing.T) {
			_, client, ctx := outboundValidationRig(t, mode.opt)
			_, err := cp.CallRaw(ctx, client, "BootNotification", []byte(`{"chargePointVendor":"V","chargePointModel":"M"}`))
			var ce *ocppj.CallError
			require.ErrorAs(t, err, &ce)
			require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)
		})
		t.Run(mode.name+"/off sends the reply as produced", func(t *testing.T) {
			_, client, ctx := outboundValidationRig(t, mode.opt, csms.WithOutboundValidation(false))
			resp, err := cp.CallRaw(ctx, client, "BootNotification", []byte(`{"chargePointVendor":"V","chargePointModel":"M"}`))
			require.NoError(t, err)
			require.NotContains(t, string(resp), `"status":"Accepted"`)
		})
		t.Run(mode.name+"/off still validates inbound requests", func(t *testing.T) {
			_, client, ctx := outboundValidationRig(t, mode.opt, csms.WithOutboundValidation(false))
			_, err := cp.CallRaw(ctx, client, "BootNotification", []byte(`{"chargePointVendor":"V"}`)) // model missing
			var ce *ocppj.CallError
			require.ErrorAs(t, err, &ce)
		})
	}
}

type cfgReq struct {
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}
type cfgResp struct {
	Status string `json:"status"`
}

var cfgMsg = ocppj.Message[cfgReq, cfgResp]{Action: "ChangeConfiguration", Direction: ocppj.SentByCSMS}

func TestOutboundValidation_OutboundCall(t *testing.T) {
	setup := func(t *testing.T, opts ...csms.Option) (*csms.Server, context.Context, chan cfgReq) {
		srv, client, ctx := outboundValidationRig(t, opts...)
		got := make(chan cfgReq, 1)
		require.NoError(t, cp.On(client, cfgMsg, func(_ context.Context, req cfgReq) (cfgResp, error) {
			got <- req
			return cfgResp{Status: "Accepted"}, nil
		}))
		require.Eventually(t, func() bool { _, ok := srv.Get("CP_OV"); return ok }, 5*time.Second, 10*time.Millisecond)
		return srv, ctx, got
	}
	// "value" is required by the schema but omitted when empty.
	invalid := cfgReq{Key: "k"}

	t.Run("default rejects an invalid outbound call", func(t *testing.T) {
		srv, ctx, got := setup(t, csms.WithStrictSchema(true))
		c, _ := srv.Get("CP_OV")
		_, err := csms.Call(ctx, c, cfgMsg, invalid)
		require.Error(t, err)
		require.Empty(t, got)
	})
	t.Run("off delivers it", func(t *testing.T) {
		srv, ctx, got := setup(t, csms.WithStrictSchema(true), csms.WithOutboundValidation(false))
		c, _ := srv.Get("CP_OV")
		resp, err := csms.Call(ctx, c, cfgMsg, invalid)
		require.NoError(t, err)
		require.Equal(t, "Accepted", resp.Status)
		require.Equal(t, invalid, <-got)
	})
}
