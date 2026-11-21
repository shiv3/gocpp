package csms_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func sessEnv(k gateway.Kind, gw string, epoch uint64) gateway.Envelope {
	e := gateway.Envelope{
		Proto: gateway.ProtoVersion, Version: ocppj.V16, Kind: k, Attempt: 1,
		Session: gateway.SessionRef{CPID: "CP_1", GatewayID: gw, Boot: "b", Epoch: epoch},
	}
	switch k {
	case gateway.KindSessionOpened:
		e.Open = &gateway.SessionInfo{}
	case gateway.KindSessionClosed:
		e.Close = &gateway.CloseInfo{Cause: gateway.SentinelName(ocppj.ErrConnClosed)}
	}
	return e
}

// With several backends a session's SessionClosed may reach one that never saw its
// SessionOpened. That backend must still report the disconnect (review B2: it
// dropped it as stale, 21 of 40 OnDisconnect missing).
func TestGateway_SessionClosedOnAnotherBackendStillFiresDisconnect(t *testing.T) {
	defer goleak.VerifyNone(t)
	var connects, disconnects atomic.Int32
	newBackend := func() *csms.Server {
		return csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"),
			csms.WithOnConnect(func(*csms.Conn) { connects.Add(1) }),
			csms.WithOnDisconnect(func(*csms.Conn, error) { disconnects.Add(1) }))
	}
	ctx := context.Background()
	for round := 0; round < 40; round++ {
		a, b := newBackend(), newBackend()
		gw := fmt.Sprintf("gw-%d", round%3)
		require.NoError(t, a.Upstream().Notify(ctx, sessEnv(gateway.KindSessionOpened, gw, 1)))
		require.NoError(t, b.Upstream().Notify(ctx, sessEnv(gateway.KindSessionClosed, gw, 1)))
		a.Close()
		b.Close()
	}
	require.EqualValues(t, 40, connects.Load())
	require.EqualValues(t, 40, disconnects.Load(), "OnDisconnect lost when the close reached another backend")
}

// A close of an already replaced session is still reported once only.
func TestGateway_LateClosedOfReplacedSessionSuppressed(t *testing.T) {
	defer goleak.VerifyNone(t)
	var disconnects atomic.Int32
	be := csms.NewServer(csms.WithGatewayLink(inproc.New().Backend()), csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnDisconnect(func(*csms.Conn, error) { disconnects.Add(1) }))
	defer be.Close()
	ctx := context.Background()
	up := be.Upstream()
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionOpened, "gw-1", 1)))
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionOpened, "gw-1", 2)))
	require.EqualValues(t, 1, disconnects.Load())
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionClosed, "gw-1", 1)))
	require.EqualValues(t, 1, disconnects.Load(), "late close of the replaced session must not fire again")
	require.NoError(t, up.Notify(ctx, sessEnv(gateway.KindSessionClosed, "gw-1", 2)))
	require.EqualValues(t, 2, disconnects.Load())
}
