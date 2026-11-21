package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

// legacyOnly hides the SessionRegistry half of a registry, so the server uses its
// legacy adapter over PutGlobal / LookupGlobal.
type legacyOnly struct{ storage.ConnectionRegistry }

// badUpgrade sends a request that looks like a WebSocket upgrade for CP_1 but is
// refused: either by the pre-check (missing key) or after it (origin).
func badUpgrade(t *testing.T, base, origin string, withKey bool) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, base+"/ocpp/CP_1", nil)
	require.NoError(t, err)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Sec-WebSocket-Version", "13")
	req.Header.Set("Sec-WebSocket-Protocol", "ocpp1.6")
	if withKey {
		req.Header.Set("Sec-WebSocket-Key", "dGhlIHNhbXBsZSBub25jZQ==")
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()
	return resp.StatusCode
}

// A failed upgrade request for a connected id must not remove the live
// connection's route (review B1).
func TestSession_FailedUpgradeKeepsLiveRoute(t *testing.T) {
	cases := []struct {
		name    string
		origin  string
		withKey bool
	}{
		{"missing key", "", false},
		{"origin refused after claim", "http://evil.example", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			reg := memory.NewSessionRegistry()
			cfg := csms.SessionConfig{GatewayID: "A"}
			srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithSessionRegistry(reg, cfg),
				csms.WithOriginPatterns("good.example"))
			ts := httptest.NewServer(srv.Handler())
			t.Cleanup(func() { ts.Close(); srv.Close() })
			a := &sessNode{srv: srv, ts: ts}
			// the live connection (no Origin header: not a browser)
			a.dial(ctx, t)
			require.Eventually(t, func() bool { _, ok := srv.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)
			before, found, err := reg.Lookup(ctx, "CP_1")
			require.NoError(t, err)
			require.True(t, found)

			require.GreaterOrEqual(t, badUpgrade(t, ts.URL, tc.origin, tc.withKey), 400)

			require.Never(t, func() bool {
				cur, ok, _ := reg.Lookup(ctx, "CP_1")
				return !ok || cur.GatewayID != "A"
			}, 300*time.Millisecond, 10*time.Millisecond, "the live connection lost its route")
			cur, _, _ := reg.Lookup(ctx, "CP_1")
			require.GreaterOrEqual(t, cur.Epoch, before.Epoch)
			_, ok := srv.Get("CP_1")
			require.True(t, ok)
		})
	}
}

// Same, when the live connection is held by another gateway: a failed upgrade at
// A must leave B's record in place.
func TestSession_FailedUpgradeKeepsOtherGatewayRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg := memory.NewSessionRegistry()
	a := newSessNode(t, reg, "A", csms.SessionConfig{})
	b := newSessNode(t, reg, "B", csms.SessionConfig{})
	b.dial(ctx, t)
	require.Eventually(t, func() bool { _, ok := b.srv.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)

	// Valid upgrade headers but a request A's origin check cannot refuse here, so
	// use a missing key: refused before the claim.
	require.GreaterOrEqual(t, badUpgrade(t, a.ts.URL, "", false), 400)

	rec, found, err := reg.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "B", rec.GatewayID)
}

// The legacy adapter (a ConnectionRegistry without sessions, WithInstanceID) must
// keep the global route of a live connection when an upgrade request for the same
// id fails.
func TestSession_LegacyRegistryFailedUpgradeKeepsLiveRoute(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	reg := legacyOnly{hub.Registry()}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInstanceID("A"),
		csms.WithConnectionRegistry(reg), csms.WithMessageRouter(hub.Router("A")))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	(&sessNode{srv: srv, ts: ts}).dial(ctx, t)
	require.Eventually(t, func() bool { _, ok := srv.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)
	owner, ok, err := reg.LookupGlobal(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, ok)
	require.Equal(t, "A", owner)

	require.GreaterOrEqual(t, badUpgrade(t, ts.URL, "", false), 400)

	owner, ok, err = reg.LookupGlobal(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, ok, "the live connection's global route was deleted")
	require.Equal(t, "A", owner)
}
