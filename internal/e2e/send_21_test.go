package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v21client "github.com/shiv3/gocpp/v21/client"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	v21p "github.com/shiv3/gocpp/v21/profiles"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"
)

// TestE2E_21NotifyPeriodicEventStreamSend exercises the OCPP 2.1 SEND message
// type (MessageTypeId 6) end to end: the charge point sends a
// NotifyPeriodicEventStream (which has no response), and the CSMS SEND handler
// receives it. The typed CP method returns only an error and does not block on a
// response, proving the unconfirmed semantics.
func TestE2E_21NotifyPeriodicEventStreamSend(t *testing.T) {
	got := make(chan v21msg.NotifyPeriodicEventStream, 1)

	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"))
	require.NoError(t, csms.OnSend(srv, v21p.NotifyPeriodicEventStream,
		func(_ context.Context, _ *csms.Conn, req v21msg.NotifyPeriodicEventStream) error {
			got <- req
			return nil
		}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1"))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	req := v21msg.NotifyPeriodicEventStream{
		ID:       42,
		Pending:  0,
		Basetime: time.Now().UTC(),
		Data: []v21msg.StreamDataElementType{
			{T: decimal.NewFromInt(0), V: "230.4"},
			{T: decimal.NewFromInt(15), V: "230.2"},
		},
	}

	// The typed CP send returns only an error: no response is awaited.
	require.NoError(t, v21client.NewCP(client).NotifyPeriodicEventStream(ctx, req))

	select {
	case rcv := <-got:
		require.EqualValues(t, 42, rcv.ID)
		require.Len(t, rcv.Data, 2)
		require.Equal(t, "230.4", rcv.Data[0].V)
	case <-time.After(2 * time.Second):
		t.Fatal("CSMS did not receive the NotifyPeriodicEventStream SEND")
	}
}
