package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/stretchr/testify/require"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func TestSessionRegistry_ClaimAssignsGrowingEpochs(t *testing.T) {
	ctx := context.Background()
	r := memory.NewSessionRegistry()
	e1, prev, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a1"})
	require.NoError(t, err)
	require.Nil(t, prev)
	e2, prev, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "B", Boot: "b1"})
	require.NoError(t, err)
	require.Greater(t, e2, e1)
	require.NotNil(t, prev)
	require.Equal(t, "A", prev.GatewayID)
	require.Equal(t, e1, prev.Epoch)

	// The epoch outlives the record: a release followed by a claim never reuses one.
	ok, err := r.Release(ctx, "CP", e2)
	require.NoError(t, err)
	require.True(t, ok)
	e3, prev, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a1"})
	require.NoError(t, err)
	require.Nil(t, prev)
	require.Greater(t, e3, e2)
}

func TestSessionRegistry_ReleaseIsCompareAndDelete(t *testing.T) {
	ctx := context.Background()
	r := memory.NewSessionRegistry()
	old, _, _ := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A"})
	cur, _, _ := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "B"})
	// A delayed release from the displaced owner must not remove the newer record.
	ok, err := r.Release(ctx, "CP", old)
	require.NoError(t, err)
	require.False(t, ok)
	s, found, _ := r.Lookup(ctx, "CP")
	require.True(t, found)
	require.Equal(t, cur, s.Epoch)
	ok, _ = r.Release(ctx, "CP", cur)
	require.True(t, ok)
	_, found, _ = r.Lookup(ctx, "CP")
	require.False(t, found)
}

func TestSessionRegistry_Reassert(t *testing.T) {
	ctx := context.Background()
	r := memory.NewSessionRegistry()
	mine := storage.Session{CPID: "CP", GatewayID: "A", Boot: "a", Epoch: 4}
	ok, err := r.Reassert(ctx, mine)
	require.NoError(t, err)
	require.True(t, ok, "absent record is restored")
	ok, _ = r.Reassert(ctx, mine)
	require.True(t, ok, "same record refreshes")
	// A later claim must be newer than the reasserted epoch.
	e, _, _ := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "B"})
	require.Greater(t, e, uint64(4))
	ok, _ = r.Reassert(ctx, mine)
	require.False(t, ok, "newer record wins")
	ok, _ = r.Reassert(ctx, storage.Session{CPID: "CP", GatewayID: "C", Epoch: e})
	require.False(t, ok, "same epoch, different owner")
}

func TestSessionRegistry_LeaseDecidesLiveness(t *testing.T) {
	ctx := context.Background()
	clk := &fakeClock{t: time.Unix(1000, 0)}
	r := memory.NewSessionRegistry(memory.WithClock(clk.now))
	g := storage.GatewayInfo{ID: "A", Boot: "boot1", State: storage.GatewayServing}
	_, _, _ = r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "boot1"})
	_, found, _ := r.Lookup(ctx, "CP")
	require.False(t, found, "no lease yet")

	require.NoError(t, r.Heartbeat(ctx, g, 15*time.Second))
	_, found, _ = r.Lookup(ctx, "CP")
	require.True(t, found)
	got, _ := r.LookupMany(ctx, []string{"CP", "other"})
	require.Len(t, got, 1)
	gws, _ := r.Gateways(ctx)
	require.Len(t, gws, 1)

	clk.advance(16 * time.Second)
	_, found, _ = r.Lookup(ctx, "CP")
	require.False(t, found, "lease expired")
	gws, _ = r.Gateways(ctx)
	require.Empty(t, gws)

	// A restarted gateway (new boot) does not revive the sessions of its old run.
	g.Boot = "boot2"
	require.NoError(t, r.Heartbeat(ctx, g, 15*time.Second))
	_, found, _ = r.Lookup(ctx, "CP")
	require.False(t, found)
}

func TestConnectionRegistry_GlobalRoutesAreSessions(t *testing.T) {
	ctx := context.Background()
	hub := memory.NewInProcessHub()
	a := hub.Registry().(storage.SessionRegistry)
	hub.Bind("CP", "B")
	s, found, err := a.Lookup(ctx, "CP")
	require.NoError(t, err)
	require.True(t, found, "a legacy record has no lease to check")
	require.Equal(t, "B", s.GatewayID)
	ok, _ := hub.Registry().(storage.OwnedGlobalDeleter).DeleteGlobalIf(ctx, "CP", "A")
	require.False(t, ok)
}

func TestSessionRegistry_AttrsMergeAndLeaseLabels(t *testing.T) {
	ctx := context.Background()
	r := memory.NewSessionRegistry()
	e, _, _ := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", GatewayVersion: "v2",
		Meta: map[string]string{"attr.site": "x", "attr.vendor": "acme", "route.pool": "default", "remote_addr": "1.2.3.4"}})
	ok, err := r.SetAttrs(ctx, "CP", e, map[string]string{"site": "y", "vendor": "", "fw": "1"})
	require.NoError(t, err)
	require.True(t, ok)
	s, _, _ := r.Lookup(ctx, "CP")
	require.Equal(t, map[string]string{"site": "y", "fw": "1"}, s.Attrs())
	require.Equal(t, "default", s.Meta["route.pool"], "other Meta keys stay")
	require.Equal(t, "v2", s.GatewayVersion)
	ok, _ = r.SetAttrs(ctx, "CP", e+1, map[string]string{"site": "z"})
	require.False(t, ok, "stale epoch")

	require.NoError(t, r.Heartbeat(ctx, storage.GatewayInfo{ID: "A", Boot: "b", Labels: map[string]string{"version": "v2", "policy_rev": "7"}}, time.Minute))
	gws, _ := r.Gateways(ctx)
	require.Equal(t, "7", gws[0].Labels["policy_rev"])
}
