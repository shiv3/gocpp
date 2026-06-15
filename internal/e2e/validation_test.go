package e2e

import (
	"context"
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

func TestE2E_StrictSchemaRejectsInvalid(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))

	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithSchemaRegistry(reg),
		csms.WithStrictSchema(true),
	)
	// Handler should never be reached for an invalid request.
	require.NoError(t, csms.On(srv, bootMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		return bootResp{Status: "Accepted"}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	// Missing required chargePointModel.
	_, err := cp.Call(ctx, client, bootMsg, bootReq{ChargePointVendor: "Acme"})
	require.Error(t, err)
	var ce *ocppj.CallError
	require.ErrorAs(t, err, &ce)
}
