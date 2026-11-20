package csms_test

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

// An inbound CALL sent right after the upgrade must not be handled before the
// connection is registered and onConnect has returned.
func TestServer_HandlerRunsAfterOnConnect(t *testing.T) {
	var connected atomic.Bool
	var handlerSawConnect, handlerSawGet atomic.Bool
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnConnect(func(*csms.Conn) {
			time.Sleep(200 * time.Millisecond)
			connected.Store(true)
		}),
	)
	msg := ocppj.Message[anyReqT, anyReqT]{Action: "Heartbeat", Direction: ocppj.SentByCP}
	require.NoError(t, csms.On(srv, msg, func(_ context.Context, c *csms.Conn, _ anyReqT) (anyReqT, error) {
		handlerSawConnect.Store(connected.Load())
		_, ok := srv.Get(c.ID())
		handlerSawGet.Store(ok)
		return anyReqT{}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/cp1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"1","Heartbeat",{}]`)))
	_, reply, err := c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(reply), `[3,"1"`)
	require.True(t, handlerSawConnect.Load(), "handler ran before onConnect returned")
	require.True(t, handlerSawGet.Load(), "handler ran before the connection was registered")
}

type anyReqT = map[string]any
