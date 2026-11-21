package dispatcher

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type connGauge struct {
	noopMetrics
	opened, closed atomic.Int64
}

func (g *connGauge) ConnectionOpened() { g.opened.Add(1) }
func (g *connGauge) ConnectionClosed() { g.closed.Add(1) }

// A connection can be closed by more than one party (a replacement closes the
// old socket while the old socket's own teardown closes it too); the connection
// gauge must drop once, or it drifts negative with every replaced connection.
func TestConn_ClosedMetricOnceWhenClosedTwice(t *testing.T) {
	defer goleak.VerifyNone(t)
	g := &connGauge{}
	cfg := DefaultConfig()
	cfg.Metrics = g
	c := NewConn("CP_1", transport.NewFakeWS("ocpp1.6"), cfg, NewHandlerRegistry())
	c.Start(context.Background())
	require.EqualValues(t, 1, g.opened.Load())
	_ = c.Close(nil)
	_ = c.Close(nil)
	require.EqualValues(t, 1, g.closed.Load())
}
