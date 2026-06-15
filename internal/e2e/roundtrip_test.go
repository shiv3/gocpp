package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

type bootReq struct {
	ChargePointVendor string `json:"chargePointVendor"`
	ChargePointModel  string `json:"chargePointModel"`
}
type bootResp struct {
	Status      string `json:"status"`
	CurrentTime string `json:"currentTime"`
	Interval    int    `json:"interval"`
}

var bootMsg = ocppj.Message[bootReq, bootResp]{Action: "BootNotification", Direction: ocppj.SentByCP}

func TestE2E_BootNotificationRoundTrip(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, csms.On(srv, bootMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		require.Equal(t, "Acme", req.ChargePointVendor)
		return bootResp{Status: "Accepted", CurrentTime: "2026-06-15T00:00:00Z", Interval: 300}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	resp, err := cp.Call(ctx, client, bootMsg, bootReq{ChargePointVendor: "Acme", ChargePointModel: "M1"})
	require.NoError(t, err)
	require.Equal(t, "Accepted", resp.Status)
	require.Equal(t, 300, resp.Interval)
}
