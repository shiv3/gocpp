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

func TestE2E_ChargingSessionGoldenPath(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))

	require.NoError(t, csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
	}))
	require.NoError(t, csms.On(srv, v16p.Authorize, func(ctx context.Context, c *csms.Conn, req v16msg.AuthorizeRequest) (v16msg.AuthorizeResponse, error) {
		return v16msg.AuthorizeResponse{IDTagInfo: v16msg.IDTagInfo{Status: v16msg.IDTagInfoStatusAccepted}}, nil
	}))
	require.NoError(t, csms.On(srv, v16p.StartTransaction, func(ctx context.Context, c *csms.Conn, req v16msg.StartTransactionRequest) (v16msg.StartTransactionResponse, error) {
		return v16msg.StartTransactionResponse{TransactionID: 1, IDTagInfo: v16msg.IDTagInfo{Status: v16msg.IDTagInfoStatusAccepted}}, nil
	}))
	require.NoError(t, csms.On(srv, v16p.MeterValues, func(ctx context.Context, c *csms.Conn, req v16msg.MeterValuesRequest) (v16msg.MeterValuesResponse, error) {
		return v16msg.MeterValuesResponse{}, nil
	}))
	require.NoError(t, csms.On(srv, v16p.StopTransaction, func(ctx context.Context, c *csms.Conn, req v16msg.StopTransactionRequest) (v16msg.StopTransactionResponse, error) {
		return v16msg.StopTransactionResponse{}, nil
	}))
	require.NoError(t, csms.On(srv, v16p.StatusNotification, func(ctx context.Context, c *csms.Conn, req v16msg.StatusNotificationRequest) (v16msg.StatusNotificationResponse, error) {
		return v16msg.StatusNotificationResponse{}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()
	cpc := v16client.NewCP(client)

	boot, err := cpc.BootNotification(ctx, v16msg.BootNotificationRequest{ChargePointVendor: "Acme", ChargePointModel: "M1"})
	require.NoError(t, err)
	require.Equal(t, v16msg.RegistrationStatusAccepted, boot.Status)

	_, err = cpc.StatusNotification(ctx, v16msg.StatusNotificationRequest{ConnectorID: 1, ErrorCode: v16msg.StatusNotificationRequestErrorCodeNoError, Status: v16msg.StatusNotificationRequestStatusPreparing})
	require.NoError(t, err)

	auth, err := cpc.Authorize(ctx, v16msg.AuthorizeRequest{IDTag: "TAG1"})
	require.NoError(t, err)
	require.Equal(t, v16msg.IDTagInfoStatusAccepted, auth.IDTagInfo.Status)

	start, err := cpc.StartTransaction(ctx, v16msg.StartTransactionRequest{ConnectorID: 1, IDTag: "TAG1", MeterStart: 0, Timestamp: time.Now()})
	require.NoError(t, err)
	require.Equal(t, int32(1), start.TransactionID)

	_, err = cpc.MeterValues(ctx, v16msg.MeterValuesRequest{ConnectorID: 1, MeterValue: []v16msg.MeterValue{{Timestamp: time.Now(), SampledValue: []v16msg.SampledValue{{Value: "100"}}}}})
	require.NoError(t, err)

	_, err = cpc.StopTransaction(ctx, v16msg.StopTransactionRequest{TransactionID: 1, MeterStop: 100, Timestamp: time.Now()})
	require.NoError(t, err)
}
