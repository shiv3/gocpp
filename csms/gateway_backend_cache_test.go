package csms

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/stretchr/testify/require"
)

// A session whose SessionClosed went to another backend would stay in this
// backend's cache forever; idle entries are dropped (review NIT: unbounded
// growth by distinct charge point ids).
func TestBackendSessionCache_IdleEntriesAreDropped(t *testing.T) {
	be := NewServer(WithGatewayLink(inproc.New().Backend()), WithSubProtocols("ocpp1.6"))
	defer be.Close()
	be.backend.cacheIdle = 50 * time.Millisecond
	up := be.Upstream()
	for i := 0; i < 200; i++ {
		require.NoError(t, up.Notify(context.Background(), gateway.Envelope{
			Proto: gateway.ProtoVersion, Version: ocppj.V16, Kind: gateway.KindSessionOpened, Attempt: 1,
			Session: gateway.SessionRef{CPID: fmt.Sprintf("CP_%d", i), GatewayID: "gw", Boot: "b", Epoch: 1},
			Open:    &gateway.SessionInfo{},
		}))
	}
	size := func() int { be.backend.mu.Lock(); defer be.backend.mu.Unlock(); return len(be.backend.sessions) }
	require.Equal(t, 200, size())
	time.Sleep(120 * time.Millisecond)
	// The next message triggers the sweep.
	require.NoError(t, up.Notify(context.Background(), gateway.Envelope{
		Proto: gateway.ProtoVersion, Version: ocppj.V16, Kind: gateway.KindSessionOpened, Attempt: 1,
		Session: gateway.SessionRef{CPID: "CP_live", GatewayID: "gw", Boot: "b", Epoch: 1}, Open: &gateway.SessionInfo{},
	}))
	require.Equal(t, 1, size(), "idle sessions stay in the cache")
	be.backend.mu.Lock()
	require.Len(t, be.backend.seen, 1)
	be.backend.mu.Unlock()
}
