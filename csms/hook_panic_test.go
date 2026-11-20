package csms_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestServer_PanickingLifecycleHooksDoNotBreakConnection(t *testing.T) {
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnConnect(func(*csms.Conn) { panic("connect boom") }),
		csms.WithOnDisconnect(func(*csms.Conn, error) { panic("disconnect boom") }),
	)
	require.NoError(t, csms.On(srv, bootMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		return bootResp{Status: "Accepted", Interval: 300}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)

	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"b1","BootNotification",{"chargePointVendor":"v","chargePointModel":"m"}]`)))
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(data), `[3,"b1"`)

	// The panicking disconnect hook must not leave the registry entry behind.
	require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
	require.Eventually(t, func() bool {
		_, ok := srv.Get("CP_1")
		return !ok
	}, 2*time.Second, 20*time.Millisecond)
}
