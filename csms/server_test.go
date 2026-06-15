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

func TestServer_AcceptsConnection(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	require.Eventually(t, func() bool {
		_, ok := srv.Get("CP_1")
		return ok
	}, 2*time.Second, 20*time.Millisecond)
}
