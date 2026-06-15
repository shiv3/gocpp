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
	"github.com/shiv3/gocpp/v201"
	"github.com/stretchr/testify/require"
)

func TestE2E_MultiVersionStrictValidation(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	require.NoError(t, v201.RegisterSchemas(reg))

	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp2.0.1", "ocpp1.6"),
		csms.WithSchemaRegistry(reg),
		csms.WithStrictSchema(true),
	)
	require.NoError(t, csms.On(srv, bootMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		return bootResp{Status: "Accepted"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_16"
	c16 := cp.NewClient("CP_16", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c16.Connect(ctx))
	defer c16.Close()

	_, err := cp.Call(ctx, c16, bootMsg, bootReq{ChargePointVendor: "Acme"})
	require.Error(t, err)
	var ce *ocppj.CallError
	require.ErrorAs(t, err, &ce)
}
