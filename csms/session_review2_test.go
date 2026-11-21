package csms_test

import (
	"context"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

// spyRegistry counts Claim and Reassert calls.
type spyRegistry struct {
	storage.SessionRegistry
	claims, reasserts atomic.Int32
}

func (s *spyRegistry) Claim(ctx context.Context, x storage.Session) (uint64, *storage.Session, error) {
	s.claims.Add(1)
	return s.SessionRegistry.Claim(ctx, x)
}

func (s *spyRegistry) Reassert(ctx context.Context, x storage.Session) (bool, error) {
	s.reasserts.Add(1)
	return s.SessionRegistry.Reassert(ctx, x)
}

// B1 pre-check alone: an upgrade request that is not a valid upgrade is refused
// before any claim, so the registry is never touched (review N12).
func TestSession_UpgradePreCheckRefusesBeforeClaim(t *testing.T) {
	spy := &spyRegistry{SessionRegistry: memory.NewSessionRegistry()}
	a := newSessNode(t, spy, "A", csms.SessionConfig{})
	require.GreaterOrEqual(t, badUpgrade(t, a.ts.URL, "", false), 400)
	require.Zero(t, spy.claims.Load(), "a request refused by the pre-check must not claim")
	require.Zero(t, spy.reasserts.Load())
}

// The legacy adapter claims after the accept only: an upgrade refused by the
// origin check never writes the global route (review N12).
func TestSession_LegacyRegistryClaimsOnlyAfterAccept(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	reg := &putSpy{ConnectionRegistry: hub.Registry()}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInstanceID("A"),
		csms.WithConnectionRegistry(legacyOnly{reg}), csms.WithMessageRouter(hub.Router("A")),
		csms.WithOriginPatterns("good.example"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	require.GreaterOrEqual(t, badUpgrade(t, ts.URL, "http://evil.example", true), 400)
	require.Zero(t, reg.puts.Load(), "a refused upgrade must not write the route")
	_, ok, err := reg.LookupGlobal(ctx, "CP_1")
	require.NoError(t, err)
	require.False(t, ok)
	// and a valid one does
	(&sessNode{srv: srv, ts: ts}).dial(ctx, t)
	require.Eventually(t, func() bool { return reg.puts.Load() > 0 }, 2*time.Second, 5*time.Millisecond)
}

type putSpy struct {
	storage.ConnectionRegistry
	puts atomic.Int32
}

func (p *putSpy) PutGlobal(ctx context.Context, cpID, instance string) error {
	p.puts.Add(1)
	return p.ConnectionRegistry.PutGlobal(ctx, cpID, instance)
}

// A failed upgrade must not restore the record of a session this gateway no
// longer holds (review N7): restoring it would route to a socket that is gone.
func TestSession_FailedUpgradeDoesNotRestoreOwnDeadRecord(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	spy := &spyRegistry{SessionRegistry: memory.NewSessionRegistry()}
	// A dead record of gateway "A" (its previous boot) for the charger.
	_, _, err := spy.Claim(ctx, storage.Session{CPID: "CP_1", GatewayID: "A", Boot: "previous-boot"})
	require.NoError(t, err)
	spy.claims.Store(0)
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithSessionRegistry(spy, csms.SessionConfig{GatewayID: "A"}),
		csms.WithOriginPatterns("good.example"))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	require.GreaterOrEqual(t, badUpgrade(t, ts.URL, "http://evil.example", true), 400)
	require.EqualValues(t, 1, spy.claims.Load(), "the origin check refuses after the claim")
	require.Zero(t, spy.reasserts.Load(), "the own gateway's dead record must not come back")
}

// reconcile must not close a live socket because the registry holds a newer
// record of this very process: a claim of an upgrade still in flight, or one
// being abandoned (review N5).
func TestSession_ReconcileKeepsSocketWhenOwnNewerClaimExists(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reg := memory.NewSessionRegistry()
	a := newSessNode(t, reg, "A", csms.SessionConfig{ReconcileInterval: 20 * time.Millisecond})
	c := a.dial(ctx, t)
	require.Eventually(t, func() bool { _, ok := a.srv.Get("CP_1"); return ok }, 2*time.Second, 5*time.Millisecond)
	conn, _ := a.srv.Get("CP_1")
	sess, ok := conn.Session()
	require.True(t, ok)
	// The claim of a concurrent upgrade of the same id at this process.
	_, _, err := reg.Claim(ctx, storage.Session{CPID: "CP_1", GatewayID: sess.GatewayID, Boot: sess.Boot})
	require.NoError(t, err)
	// Several reconcile passes run meanwhile; the socket must stay open.
	rctx, rcancel := context.WithTimeout(ctx, 400*time.Millisecond)
	defer rcancel()
	_, _, err = c.Read(rctx)
	require.ErrorIs(t, err, context.DeadlineExceeded, "reconcile closed the live socket for its own in-flight claim")
}
