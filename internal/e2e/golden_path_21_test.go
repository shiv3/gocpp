package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	v21p "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

func TestE2E_21ChargingSessionGoldenPath(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"))
	transactionEvents := make(chan string, 2)

	require.NoError(t, csms.On(srv, v21p.BootNotification, func(ctx context.Context, c *csms.Conn, req v21msg.BootNotificationRequest) (v21msg.BootNotificationResponse, error) {
		require.Equal(t, "PowerUp", req.Reason)
		require.Equal(t, "Acme", req.ChargingStation.VendorName)
		return v21msg.BootNotificationResponse{CurrentTime: time.Now().UTC(), Interval: 300, Status: "Accepted"}, nil
	}))
	require.NoError(t, csms.On(srv, v21p.Authorize, func(ctx context.Context, c *csms.Conn, req v21msg.AuthorizeRequest) (v21msg.AuthorizeResponse, error) {
		require.Equal(t, "TAG1", req.IDToken.IDToken)
		return v21msg.AuthorizeResponse{IDTokenInfo: v21msg.IdTokenInfoType{Status: "Accepted"}}, nil
	}))
	require.NoError(t, csms.On(srv, v21p.TransactionEvent, func(ctx context.Context, c *csms.Conn, req v21msg.TransactionEventRequest) (v21msg.TransactionEventResponse, error) {
		require.Equal(t, "tx-21-1", req.TransactionInfo.TransactionID)
		transactionEvents <- req.EventType
		return v21msg.TransactionEventResponse{}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_21"
	client := cp.NewClient("CP_21", url, cp.WithSubProtocols("ocpp2.1"))

	// CP-side handler for a 2.1-specific CSMS-originated message.
	derTransfer := make(chan string, 1)
	require.NoError(t, cp.On(client, v21p.NotifyAllowedEnergyTransfer, func(ctx context.Context, req v21msg.NotifyAllowedEnergyTransferRequest) (v21msg.NotifyAllowedEnergyTransferResponse, error) {
		derTransfer <- req.TransactionID
		return v21msg.NotifyAllowedEnergyTransferResponse{Status: "Accepted"}, nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()
	require.Equal(t, "ocpp2.1", client.NegotiatedProtocol())

	boot, err := cp.Call(ctx, client, v21p.BootNotification, v21msg.BootNotificationRequest{
		ChargingStation: v21msg.ChargingStationType{VendorName: "Acme", Model: "M21"},
		Reason:          "PowerUp",
	})
	require.NoError(t, err)
	require.Equal(t, "Accepted", boot.Status)

	// 2.1-specific message: CSMS pushes NotifyAllowedEnergyTransfer to the CP.
	conn, ok := srv.Get("CP_21")
	require.True(t, ok)
	naet, err := csms.Call(ctx, conn, v21p.NotifyAllowedEnergyTransfer, v21msg.NotifyAllowedEnergyTransferRequest{
		AllowedEnergyTransfer: []string{"AC_single_phase"},
		TransactionID:         "tx-21-1",
	})
	require.NoError(t, err)
	require.Equal(t, "Accepted", naet.Status)
	require.Equal(t, "tx-21-1", <-derTransfer)

	auth, err := cp.Call(ctx, client, v21p.Authorize, v21msg.AuthorizeRequest{
		IDToken: v21msg.IdTokenType{IDToken: "TAG1", Type: "ISO14443"},
	})
	require.NoError(t, err)
	require.Equal(t, "Accepted", auth.IDTokenInfo.Status)

	idToken := v21msg.IdTokenType{IDToken: "TAG1", Type: "ISO14443"}
	_, err = cp.Call(ctx, client, v21p.TransactionEvent, v21msg.TransactionEventRequest{
		EventType:       "Started",
		IDToken:         &idToken,
		SeqNo:           1,
		Timestamp:       time.Now().UTC(),
		TransactionInfo: v21msg.TransactionType{TransactionID: "tx-21-1"},
		TriggerReason:   "Authorized",
	})
	require.NoError(t, err)
	require.Equal(t, "Started", <-transactionEvents)

	stoppedReason := "EVDisconnected"
	_, err = cp.Call(ctx, client, v21p.TransactionEvent, v21msg.TransactionEventRequest{
		EventType:       "Ended",
		SeqNo:           2,
		Timestamp:       time.Now().UTC(),
		TransactionInfo: v21msg.TransactionType{StoppedReason: &stoppedReason, TransactionID: "tx-21-1"},
		TriggerReason:   "StopAuthorized",
	})
	require.NoError(t, err)
	require.Equal(t, "Ended", <-transactionEvents)
}
