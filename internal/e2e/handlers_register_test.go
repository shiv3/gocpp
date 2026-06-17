package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// testCSMSHandler implements only BootNotification; everything else falls back
// to the generated Unimplemented default.
type testCSMSHandler struct {
	handlers.UnimplementedCSMSHandler
}

func (testCSMSHandler) OnBootNotification(_ context.Context, _ *csms.Conn, _ v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
	return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
}

// testCPHandler implements only Reset; everything else falls back to the
// generated Unimplemented default.
type testCPHandler struct {
	handlers.UnimplementedCPHandler
	resetCalled bool
}

func (h *testCPHandler) OnReset(_ context.Context, _ v16msg.ResetRequest) (v16msg.ResetResponse, error) {
	h.resetCalled = true
	return v16msg.ResetResponse{Status: v16msg.ResetResponseStatusAccepted}, nil
}

func TestE2E_RegisterHandlers(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, handlers.RegisterCSMS(srv, testCSMSHandler{}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	cpHandler := &testCPHandler{}
	require.NoError(t, handlers.RegisterCP(client, cpHandler))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	// Implemented CP->CSMS handler responds.
	bootResp, err := cp.Call(ctx, client, v16p.BootNotification, v16msg.BootNotificationRequest{
		ChargePointVendor: "Acme", ChargePointModel: "M1",
	})
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

	// Implemented CSMS->CP handler responds.
	resetResp, err := csms.Call(ctx, conn, v16p.Reset, v16msg.ResetRequest{Type: v16msg.ResetRequestTypeSoft})
	require.NoError(t, err)
	require.Equal(t, v16msg.ResetResponseStatusAccepted, resetResp.Status)
	require.True(t, cpHandler.resetCalled)

	// Unimplemented CSMS->CP message returns a NotSupported CallError.
	_, err = csms.Call(ctx, conn, v16p.ChangeConfiguration, v16msg.ChangeConfigurationRequest{Key: "k", Value: "v"})
	require.Error(t, err)
	var callErr *ocppj.CallError
	require.ErrorAs(t, err, &callErr)
	require.Equal(t, ocppj.ErrorCodeNotSupported, callErr.Code)
}
