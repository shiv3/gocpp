package splitharness_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/addons/link-nats/splitharness"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type hbReq struct{}
type hbResp struct {
	CurrentTime string `json:"currentTime"`
}

var hbMsg = ocppj.Message[hbReq, hbResp]{Action: "Heartbeat", Direction: ocppj.SentByCP}

// A server built with the harness link really talks over NATS: the backend is
// listed by the NATS gateway end, a call round-trips, and Close releases every
// connection and the embedded broker.
func TestHarnessLinkCarriesTraffic(t *testing.T) {
	defer goleak.VerifyNone(t)
	link := splitharness.New()
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithSplitLink(link))
	require.NoError(t, csms.On(srv, hbMsg, func(context.Context, *csms.Conn, hbReq) (hbResp, error) {
		return hbResp{CurrentTime: "2026-01-01T00:00:00Z"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	pools, ok := link.Gateway().(interface{ Available(string) bool })
	require.True(t, ok, "the gateway end is the NATS gateway")
	require.True(t, pools.Available("default"))

	c := cp.NewClient("CP_1", "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	resp, err := cp.Call(ctx, c, hbMsg, hbReq{})
	require.NoError(t, err)
	require.Equal(t, "2026-01-01T00:00:00Z", resp.CurrentTime)
	c.Close()
	srv.Close()
}
