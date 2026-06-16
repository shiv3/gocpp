package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestLenientSchema_ExtraFieldAndEnumCase(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))

	bootHandled := make(chan struct{}, 1)
	statusSeen := make(chan v16msg.StatusNotificationRequestStatus, 1)
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithSchemaRegistry(reg),
		csms.WithLenientSchema(),
	)
	require.NoError(t, csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		bootHandled <- struct{}{}
		return v16msg.BootNotificationResponse{
			Status:      v16msg.RegistrationStatusAccepted,
			CurrentTime: time.Now().UTC(),
			Interval:    300,
		}, nil
	}))
	require.NoError(t, csms.On(srv, v16p.StatusNotification, func(ctx context.Context, c *csms.Conn, req v16msg.StatusNotificationRequest) (v16msg.StatusNotificationResponse, error) {
		statusSeen <- req.Status
		return v16msg.StatusNotificationResponse{}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	resp, err := cp.CallRaw(ctx, client, "BootNotification",
		[]byte(`{"chargePointVendor":"Acme","chargePointModel":"M1","extra":1}`))
	require.NoError(t, err)
	require.Contains(t, string(resp), `"status":"Accepted"`)
	select {
	case <-bootHandled:
	default:
		t.Fatal("BootNotification handler was not reached")
	}

	_, err = cp.CallRaw(ctx, client, "StatusNotification",
		[]byte(`{"connectorId":1,"errorCode":"NoError","status":"preparing"}`))
	require.NoError(t, err)
	select {
	case got := <-statusSeen:
		require.Equal(t, v16msg.StatusNotificationRequestStatusPreparing, got)
	default:
		t.Fatal("StatusNotification handler was not reached")
	}
}
