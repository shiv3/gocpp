package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v201msg "github.com/shiv3/gocpp/v201/messages"
	v201p "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func TestE2E_201TransactionEventGoldenPath(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.0.1"))
	transactionEvents := make(chan string, 2)

	require.NoError(t, csms.On(srv, v201p.BootNotification, func(ctx context.Context, c *csms.Conn, req v201msg.BootNotificationRequest) (v201msg.BootNotificationResponse, error) {
		require.Equal(t, "PowerUp", req.Reason)
		require.Equal(t, "Acme", req.ChargingStation.VendorName)
		require.Equal(t, "M201", req.ChargingStation.Model)
		return v201msg.BootNotificationResponse{CurrentTime: time.Now().UTC(), Interval: 300, Status: "Accepted"}, nil
	}))
	require.NoError(t, csms.On(srv, v201p.Authorize, func(ctx context.Context, c *csms.Conn, req v201msg.AuthorizeRequest) (v201msg.AuthorizeResponse, error) {
		require.Equal(t, "TAG1", req.IDToken.IDToken)
		require.Equal(t, "ISO14443", req.IDToken.Type)
		return v201msg.AuthorizeResponse{IDTokenInfo: v201msg.IdTokenInfoType{Status: "Accepted"}}, nil
	}))
	require.NoError(t, csms.On(srv, v201p.TransactionEvent, func(ctx context.Context, c *csms.Conn, req v201msg.TransactionEventRequest) (v201msg.TransactionEventResponse, error) {
		require.Equal(t, "tx-201-1", req.TransactionInfo.TransactionID)
		switch req.EventType {
		case "Started":
			require.Equal(t, "Authorized", req.TriggerReason)
			require.NotNil(t, req.IDToken)
			require.Equal(t, "TAG1", req.IDToken.IDToken)
		case "Ended":
			require.Equal(t, "StopAuthorized", req.TriggerReason)
			require.NotNil(t, req.TransactionInfo.StoppedReason)
			require.Equal(t, "EVDisconnected", *req.TransactionInfo.StoppedReason)
		default:
			t.Fatalf("unexpected transaction event type %q", req.EventType)
		}
		transactionEvents <- req.EventType
		return v201msg.TransactionEventResponse{}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_201"
	client := cp.NewClient("CP_201", url, cp.WithSubProtocols("ocpp2.0.1"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()
	require.Equal(t, "ocpp2.0.1", client.NegotiatedProtocol())

	boot, err := cp.Call(ctx, client, v201p.BootNotification, v201msg.BootNotificationRequest{
		ChargingStation: v201msg.ChargingStationType{VendorName: "Acme", Model: "M201"},
		Reason:          "PowerUp",
	})
	require.NoError(t, err)
	require.Equal(t, "Accepted", boot.Status)

	auth, err := cp.Call(ctx, client, v201p.Authorize, v201msg.AuthorizeRequest{
		IDToken: v201msg.IdTokenType{IDToken: "TAG1", Type: "ISO14443"},
	})
	require.NoError(t, err)
	require.Equal(t, "Accepted", auth.IDTokenInfo.Status)

	connectorID := int32(1)
	idToken := v201msg.IdTokenType{IDToken: "TAG1", Type: "ISO14443"}
	_, err = cp.Call(ctx, client, v201p.TransactionEvent, v201msg.TransactionEventRequest{
		EventType:       "Started",
		EVSE:            &v201msg.EVSEType{ID: 1, ConnectorID: &connectorID},
		IDToken:         &idToken,
		SeqNo:           1,
		Timestamp:       time.Now().UTC(),
		TransactionInfo: v201msg.TransactionType{TransactionID: "tx-201-1"},
		TriggerReason:   "Authorized",
	})
	require.NoError(t, err)
	require.Equal(t, "Started", <-transactionEvents)

	stoppedReason := "EVDisconnected"
	_, err = cp.Call(ctx, client, v201p.TransactionEvent, v201msg.TransactionEventRequest{
		EventType: "Ended",
		SeqNo:     2,
		Timestamp: time.Now().UTC(),
		TransactionInfo: v201msg.TransactionType{
			StoppedReason: &stoppedReason,
			TransactionID: "tx-201-1",
		},
		TriggerReason: "StopAuthorized",
	})
	require.NoError(t, err)
	require.Equal(t, "Ended", <-transactionEvents)
}
