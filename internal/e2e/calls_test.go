package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/calls"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// TestE2E_Calls exercises the generated typed send helpers in both directions.
func TestE2E_Calls(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, csms.On(srv, v16p.BootNotification, func(_ context.Context, _ *csms.Conn, _ v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	require.NoError(t, cp.On(client, v16p.Reset, func(_ context.Context, _ v16msg.ResetRequest) (v16msg.ResetResponse, error) {
		return v16msg.ResetResponse{Status: v16msg.ResetResponseStatusAccepted}, nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	// CP -> CSMS via generated send helper.
	bootResp, err := calls.CPBootNotification(ctx, client, v16msg.BootNotificationRequest{ChargePointVendor: "Acme", ChargePointModel: "M1"})
	require.NoError(t, err)
	require.Equal(t, v16msg.RegistrationStatusAccepted, bootResp.Status)

	var conn *csms.Conn
	require.Eventually(t, func() bool {
		c, ok := srv.Get("CP_1")
		if ok {
			conn = c
		}
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	// CSMS -> CP via generated send helper.
	resetResp, err := calls.CSMSReset(ctx, conn, v16msg.ResetRequest{Type: v16msg.ResetRequestTypeSoft})
	require.NoError(t, err)
	require.Equal(t, v16msg.ResetResponseStatusAccepted, resetResp.Status)
}
