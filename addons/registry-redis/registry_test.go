package registryredis_test

import (
	"context"
	"net"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/stretchr/testify/require"
)

// backend runs a test against an embedded miniredis and, when GOCPP_REDIS_ADDR
// names a real server, against that as well. fastForward moves TTLs on the
// embedded server only; the real-server run skips what needs it.
type backend struct {
	name        string
	client      func(t *testing.T) *redis.Client
	fastForward func(d time.Duration)
	prefix      string
}

func backends(t *testing.T) []backend {
	t.Helper()
	mr := miniredis.RunT(t)
	out := []backend{{
		name:        "miniredis",
		client:      func(t *testing.T) *redis.Client { return redis.NewClient(&redis.Options{Addr: mr.Addr()}) },
		fastForward: mr.FastForward,
		prefix:      "t",
	}}
	if addr := os.Getenv("GOCPP_REDIS_ADDR"); addr != "" {
		out = append(out, backend{
			name:   "redis",
			client: func(t *testing.T) *redis.Client { return redis.NewClient(&redis.Options{Addr: addr}) },
			prefix: "gocpp:test:" + time.Now().Format("150405.000000000"),
		})
	}
	return out
}

func eachBackend(t *testing.T, fn func(t *testing.T, b backend)) {
	for _, b := range backends(t) {
		t.Run(b.name, func(t *testing.T) { fn(t, b) })
	}
}

func newReg(t *testing.T, b backend) (*registryredis.Registry, *redis.Client) {
	t.Helper()
	c := b.client(t)
	t.Cleanup(func() { _ = c.Close() })
	return registryredis.New(c, registryredis.WithKeyPrefix(b.prefix)), c
}

func TestClaimReleaseReassert(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		r, _ := newReg(t, b)
		at := time.Unix(1_700_000_000, 0).UTC()
		e1, prev, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a1", Version: "ocpp1.6", ConnectedAt: at, Meta: map[string]string{"k": "v"}})
		require.NoError(t, err)
		require.Nil(t, prev)
		require.Equal(t, uint64(1), e1)

		e2, prev, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "B", Boot: "b1"})
		require.NoError(t, err)
		require.Equal(t, uint64(2), e2)
		require.NotNil(t, prev)
		require.Equal(t, "A", prev.GatewayID)
		require.Equal(t, e1, prev.Epoch)
		require.Equal(t, "ocpp1.6", prev.Version)
		require.True(t, at.Equal(prev.ConnectedAt))
		require.Equal(t, map[string]string{"k": "v"}, prev.Meta)

		ok, err := r.Release(ctx, "CP", e1)
		require.NoError(t, err)
		require.False(t, ok, "release with a stale epoch is a no-op")
		ok, err = r.Release(ctx, "CP", e2)
		require.NoError(t, err)
		require.True(t, ok)

		e3, prev, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a1"})
		require.NoError(t, err)
		require.Nil(t, prev)
		require.Equal(t, uint64(3), e3, "the epoch survives the record")

		// Reassert: absent record restored, later claims stay ahead.
		other := storage.Session{CPID: "CP2", GatewayID: "A", Boot: "a1", Epoch: 7}
		ok, err = r.Reassert(ctx, other)
		require.NoError(t, err)
		require.True(t, ok)
		ok, _ = r.Reassert(ctx, other)
		require.True(t, ok, "refresh")
		e, _, _ := r.Claim(ctx, storage.Session{CPID: "CP2", GatewayID: "B", Boot: "b1"})
		require.Equal(t, uint64(8), e)
		ok, _ = r.Reassert(ctx, other)
		require.False(t, ok, "newer record wins")
		ok, _ = r.Reassert(ctx, storage.Session{CPID: "CP2", GatewayID: "C", Boot: "c", Epoch: 8})
		require.False(t, ok, "same epoch, different owner")
	})
}

func TestLeaseDecidesLiveness(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		if b.fastForward == nil {
			t.Skip("needs TTL control")
		}
		ctx := context.Background()
		r, _ := newReg(t, b)
		g := storage.GatewayInfo{ID: "A", Boot: "boot1", State: storage.GatewayServing, Sessions: 2}
		_, _, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "boot1"})
		require.NoError(t, err)
		_, found, _ := r.Lookup(ctx, "CP")
		require.False(t, found, "no lease yet")

		require.NoError(t, r.Heartbeat(ctx, g, 15*time.Second))
		s, found, err := r.Lookup(ctx, "CP")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "A", s.GatewayID)
		got, err := r.LookupMany(ctx, []string{"CP", "missing"})
		require.NoError(t, err)
		require.Len(t, got, 1)
		gws, err := r.Gateways(ctx)
		require.NoError(t, err)
		require.Equal(t, []storage.GatewayInfo{g}, gws)

		b.fastForward(16 * time.Second)
		_, found, _ = r.Lookup(ctx, "CP")
		require.False(t, found, "lease expired: the whole gateway's sessions are dead at once")

		g.Boot = "boot2" // restarted gateway
		require.NoError(t, r.Heartbeat(ctx, g, 15*time.Second))
		_, found, _ = r.Lookup(ctx, "CP")
		require.False(t, found, "a new boot does not revive the old run's sessions")
	})
}

// latencyHook delays every command, so one client plays the writer whose
// request is slow on the wire.
type latencyHook struct{ d time.Duration }

func (h latencyHook) DialHook(next redis.DialHook) redis.DialHook {
	return func(ctx context.Context, network, addr string) (net.Conn, error) { return next(ctx, network, addr) }
}

func (h latencyHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		time.Sleep(h.d)
		return next(ctx, cmd)
	}
}

func (h latencyHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []redis.Cmder) error {
		time.Sleep(h.d)
		return next(ctx, cmds)
	}
}

// The stale-writer case documented for PutGlobal: instance A's write for an old
// connection is delayed past instance B's write for the newer one. With
// PutGlobal the late write wins. Here A's delayed Release (its socket ended)
// lands after B's claim and removes nothing.
func TestStaleWriterCannotOverwriteNewerConnection(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		fast, _ := newReg(t, b)
		slowClient := b.client(t)
		slowClient.AddHook(latencyHook{d: 300 * time.Millisecond})
		t.Cleanup(func() { _ = slowClient.Close() })
		slow := registryredis.New(slowClient, registryredis.WithKeyPrefix(b.prefix))

		eA, _, err := fast.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a"})
		require.NoError(t, err)

		released := make(chan bool, 1)
		go func() { // A's socket ended; its release is on the wire
			ok, _ := slow.Release(ctx, "CP", eA)
			released <- ok
		}()
		time.Sleep(50 * time.Millisecond)
		eB, prev, err := fast.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "B", Boot: "b"})
		require.NoError(t, err)
		require.NotNil(t, prev)
		require.Greater(t, eB, eA)

		require.False(t, <-released, "the delayed release must not remove B's record")
		require.NoError(t, fast.Heartbeat(ctx, storage.GatewayInfo{ID: "B", Boot: "b"}, time.Minute))
		s, found, err := fast.Lookup(ctx, "CP")
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "B", s.GatewayID)

		// A delayed Reassert of the old session cannot resurrect it either.
		ok, err := slow.Reassert(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a", Epoch: eA})
		require.NoError(t, err)
		require.False(t, ok)
	})
}

func TestAttrsAndGatewayRelease(t *testing.T) {
	eachBackend(t, func(t *testing.T, b backend) {
		ctx := context.Background()
		r, _ := newReg(t, b)
		e, _, err := r.Claim(ctx, storage.Session{CPID: "CP", GatewayID: "A", Boot: "a", GatewayVersion: "v2", GatewayLabel: "canary",
			Attrs: map[string]string{"site": "x"}})
		require.NoError(t, err)
		require.NoError(t, r.Heartbeat(ctx, storage.GatewayInfo{ID: "A", Boot: "a", Version: "v2", Label: "canary"}, time.Minute))
		s, found, _ := r.Lookup(ctx, "CP")
		require.True(t, found)
		require.Equal(t, map[string]string{"site": "x"}, s.Attrs)
		require.Equal(t, "v2", s.GatewayVersion)
		require.Equal(t, "canary", s.GatewayLabel)
		ok, err := r.SetAttrs(ctx, "CP", e, map[string]string{"site": "y", "fw": "1"})
		require.NoError(t, err)
		require.True(t, ok)
		ok, _ = r.SetAttrs(ctx, "CP", e+1, nil)
		require.False(t, ok)
		s, _, _ = r.Lookup(ctx, "CP")
		require.Equal(t, map[string]string{"site": "y", "fw": "1"}, s.Attrs)
		gws, _ := r.Gateways(ctx)
		require.Equal(t, "canary", gws[0].Label)
	})
}
