package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v16client "github.com/shiv3/gocpp/v16/client"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// TestE2E_16GeneratedClientMethods exercises the generated wrapper types: a CP
// method (CP.BootNotification) and a CSMS method (CSMS.Reset), confirming each
// behaves like the equivalent cp.Call / csms.Call.
func TestE2E_16GeneratedClientMethods(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))

	require.NoError(t, csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		require.Equal(t, "Acme", req.ChargePointVendor)
		return v16msg.BootNotificationResponse{CurrentTime: time.Now().UTC(), Interval: 300, Status: "Accepted"}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_16"
	rawClient := cp.NewClient("CP_16", url, cp.WithSubProtocols("ocpp1.6"))

	resetReason := make(chan v16msg.ResetRequestType, 1)
	require.NoError(t, cp.On(rawClient, v16p.Reset, func(ctx context.Context, req v16msg.ResetRequest) (v16msg.ResetResponse, error) {
		resetReason <- req.Type
		return v16msg.ResetResponse{Status: v16msg.ResetResponseStatusAccepted}, nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, rawClient.Connect(ctx))
	defer rawClient.Close()

	// CP-side method: charge point -> CSMS.
	cpc := v16client.NewCP(rawClient)
	boot, err := cpc.BootNotification(ctx, v16msg.BootNotificationRequest{
		ChargePointVendor: "Acme",
		ChargePointModel:  "M16",
	})
	require.NoError(t, err)
	require.Equal(t, v16msg.RegistrationStatusAccepted, boot.Status)
	// Embedded *cp.Client surface stays available through the wrapper.
	require.True(t, cpc.IsConnected())

	// CSMS-side method: CSMS -> charge point.
	conn, ok := srv.Get("CP_16")
	require.True(t, ok)
	reset, err := v16client.NewCSMS(conn).Reset(ctx, v16msg.ResetRequest{Type: v16msg.ResetRequestTypeSoft})
	require.NoError(t, err)
	require.Equal(t, v16msg.ResetResponseStatusAccepted, reset.Status)
	require.Equal(t, v16msg.ResetRequestTypeSoft, <-resetReason)
}
