package csms_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

type sessNode struct {
	srv *csms.Server
	ts  *httptest.Server
}

func newSessNode(t *testing.T, reg storage.SessionRegistry, id string, cfg csms.SessionConfig) *sessNode {
	t.Helper()
	cfg.GatewayID = id
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithSessionRegistry(reg, cfg))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return &sessNode{srv: srv, ts: ts}
}

func (n *sessNode) dial(ctx context.Context, t *testing.T) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, "ws"+n.ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func TestSession_ClaimedBeforeAcceptAndReleasedOnClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg := memory.NewSessionRegistry()
	a := newSessNode(t, reg, "A", csms.SessionConfig{})
	c := a.dial(ctx, t)

	conn, ok := a.srv.Get("CP_1")
	require.True(t, ok)
	sess, ok := conn.Session()
	require.True(t, ok)
	require.NotZero(t, sess.Epoch)
	require.Equal(t, "A", sess.GatewayID)
	rec, found, err := reg.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, sess.Epoch, rec.Epoch)

	_ = c.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, f, _ := reg.Lookup(ctx, "CP_1"); return !f }, 2*time.Second, 10*time.Millisecond)
}

func TestSession_ReconnectToOtherGatewayDisplacesOldSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reg := memory.NewSessionRegistry()
	cfg := csms.SessionConfig{ReconcileInterval: 50 * time.Millisecond, HeartbeatInterval: 20 * time.Millisecond}
	a, b := newSessNode(t, reg, "A", cfg), newSessNode(t, reg, "B", cfg)

	old := a.dial(ctx, t)
	aConn, _ := a.srv.Get("CP_1")
	oldSess, _ := aConn.Session()
	b.dial(ctx, t)

	// The registry now routes to B...
	rec, found, err := reg.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, "B", rec.GatewayID)
	require.Greater(t, rec.Epoch, oldSess.Epoch)
	// ... A's reconcile pass closes the superseded socket (1008) ...
	_, _, err = old.Read(ctx)
	var ce websocket.CloseError
	require.True(t, errors.As(err, &ce), "got %v", err)
	require.Equal(t, websocket.StatusPolicyViolation, ce.Code)
	require.Eventually(t, func() bool { _, ok := a.srv.Get("CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)
	// ... and A's late release does not remove B's record.
	time.Sleep(50 * time.Millisecond)
	rec, found, _ = reg.Lookup(ctx, "CP_1")
	require.True(t, found)
	require.Equal(t, "B", rec.GatewayID)

	h, err := a.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, h.Remote())
	hs, ok := h.Session()
	require.True(t, ok)
	require.Equal(t, rec.Epoch, hs.Epoch)
}

// slowRegistry delays Claim until released, to simulate a registry outage.
type slowRegistry struct {
	storage.SessionRegistry
	block atomic.Bool
}

func (s *slowRegistry) Claim(ctx context.Context, sess storage.Session) (uint64, *storage.Session, error) {
	if s.block.Load() {
		<-ctx.Done()
		return 0, nil, ctx.Err()
	}
	return s.SessionRegistry.Claim(ctx, sess)
}

func TestSession_RegistryOutageAcceptsUnclaimedThenClaims(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	reg := &slowRegistry{SessionRegistry: memory.NewSessionRegistry()}
	reg.block.Store(true)
	a := newSessNode(t, reg, "A", csms.SessionConfig{ClaimTimeout: 100 * time.Millisecond})
	a.dial(ctx, t)

	conn, ok := a.srv.Get("CP_1")
	require.True(t, ok, "connection served while the registry is unreachable")
	sess, ok := conn.Session()
	require.True(t, ok)
	require.Zero(t, sess.Epoch)

	reg.block.Store(false)
	require.Eventually(t, func() bool { s, _ := conn.Session(); return s.Epoch != 0 }, 5*time.Second, 20*time.Millisecond,
		"unclaimed session claimed in the background")
	_, found, _ := reg.Lookup(ctx, "CP_1")
	require.True(t, found)
}

func TestSession_LegacyAdapterKeepsInstanceIDBehaviour(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	reg := plainRegistry{hub.Registry()} // hides SessionRegistry and OwnedGlobalDeleter
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInstanceID("A"),
		csms.WithConnectionRegistry(reg), csms.WithMessageRouter(hub.Router("A")))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	require.Eventually(t, func() bool { o, ok, _ := reg.LookupGlobal(ctx, "CP_1"); return ok && o == "A" }, 2*time.Second, 10*time.Millisecond)
	_ = c.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok, _ := reg.LookupGlobal(ctx, "CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)
}

func TestSession_NotClusteredHasNoSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	conn, ok := srv.Get("CP_1")
	require.True(t, ok)
	_, has := conn.Session()
	require.False(t, has)
	_, err = srv.Lookup(ctx, "OTHER")
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

func TestSession_NoClaimForRejectedUpgrade(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg := &countingRegistry{SessionRegistry: memory.NewSessionRegistry()}
	a := newSessNode(t, reg, "A", csms.SessionConfig{})
	// No common subprotocol: the upgrade is accepted and closed, but never claimed.
	c, _, err := websocket.Dial(ctx, "ws"+a.ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp2.0.1"}})
	if err == nil {
		_ = c.CloseNow()
	}
	time.Sleep(50 * time.Millisecond)
	require.Zero(t, reg.claims.Load())
}

type countingRegistry struct {
	storage.SessionRegistry
	claims atomic.Int32
}

func (c *countingRegistry) Claim(ctx context.Context, s storage.Session) (uint64, *storage.Session, error) {
	c.claims.Add(1)
	return c.SessionRegistry.Claim(ctx, s)
}
