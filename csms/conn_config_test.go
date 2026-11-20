package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestServer_ConnConfigOverridesPerChargePoint(t *testing.T) {
	var defaults csms.ConnTimeouts
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithConnConfig(func(r *http.Request, cpID string, tm *csms.ConnTimeouts) {
			if cpID == "SHORT" {
				require.Equal(t, "yes", r.Header.Get("X-Probe"))
				defaults = *tm
				tm.ReadTimeout = 150 * time.Millisecond
			}
		}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(id string) {
		h := http.Header{"X-Probe": {"yes"}}
		c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/"+id,
			&websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, HTTPHeader: h})
		require.NoError(t, err)
		t.Cleanup(func() { _ = c.CloseNow() })
		// Reading lets the client answer the server's close handshake.
		go func() { _, _, _ = c.Read(ctx) }()
	}
	dial("SHORT")
	dial("LONG")
	require.Eventually(t, func() bool {
		_, a := srv.Get("SHORT")
		_, b := srv.Get("LONG")
		return a && b
	}, 2*time.Second, 10*time.Millisecond)

	// The hook saw the server-wide defaults, not zeros.
	require.Equal(t, 60*time.Second, defaults.ReadTimeout)
	require.Equal(t, 30*time.Second, defaults.CallTimeout)

	// The overridden connection idles out; the default one stays up.
	require.Eventually(t, func() bool {
		_, ok := srv.Get("SHORT")
		return !ok
	}, 3*time.Second, 20*time.Millisecond)
	_, ok := srv.Get("LONG")
	require.True(t, ok)
}
