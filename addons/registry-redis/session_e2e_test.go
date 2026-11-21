package registryredis_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

// Two CSMS instances over one Redis registry: a charge point that reconnects to
// the other instance moves the route, and the superseded socket is closed.
func TestServersShareRoutesThroughRedis(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cfg := csms.SessionConfig{ReconcileInterval: 50 * time.Millisecond, HeartbeatInterval: 50 * time.Millisecond}
		mk := func(id string) (*csms.Server, *httptest.Server) {
			r, _ := newReg(t, b)
			c := cfg
			c.GatewayID = id
			srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithSessionRegistry(r, c))
			ts := httptest.NewServer(srv.Handler())
			t.Cleanup(func() { ts.Close(); srv.Close() })
			return srv, ts
		}
		a, tsA := mk("A")
		_, tsB := mk("B")
		dial := func(ts *httptest.Server) *websocket.Conn {
			c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
			require.NoError(t, err)
			t.Cleanup(func() { _ = c.CloseNow() })
			return c
		}
		old := dial(tsA)
		require.Eventually(t, func() bool { _, ok := a.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)
		conn, ok := a.Get("CP_1")
		require.True(t, ok)
		s1, _ := conn.Session()
		dial(tsB)

		_, _, err := old.Read(ctx)
		var ce websocket.CloseError
		require.True(t, errors.As(err, &ce), "got %v", err)
		require.Equal(t, websocket.StatusPolicyViolation, ce.Code)

		require.Eventually(t, func() bool { _, ok := a.Get("CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)
		h, err := a.Lookup(ctx, "CP_1")
		require.NoError(t, err)
		require.True(t, h.Remote())
		s2, _ := h.Session()
		require.Equal(t, "B", s2.GatewayID)
		require.Greater(t, s2.Epoch, s1.Epoch)
	})
}
