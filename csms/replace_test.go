package csms_test

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestServer_ReplacedConnectionLifecycle(t *testing.T) {
	var (
		mu     sync.Mutex
		events []string
		conns  []*csms.Conn
		errs   []error
	)
	idx := func(c *csms.Conn) int {
		for i, x := range conns {
			if x == c {
				return i + 1
			}
		}
		conns = append(conns, c)
		return len(conns)
	}
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnConnect(func(c *csms.Conn) {
			mu.Lock()
			defer mu.Unlock()
			events = append(events, fmt.Sprintf("connect(%d)", idx(c)))
		}),
		csms.WithOnDisconnect(func(c *csms.Conn, err error) {
			// Slow callback: the new connect must still wait for it.
			time.Sleep(50 * time.Millisecond)
			mu.Lock()
			defer mu.Unlock()
			events = append(events, fmt.Sprintf("disconnect(%d)", idx(c)))
			errs = append(errs, err)
		}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/cp1"
	dial := func() *websocket.Conn {
		c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		require.NoError(t, err)
		return c
	}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}

	c1 := dial()
	require.Eventually(t, func() bool { return len(snapshot()) == 1 }, 2*time.Second, 10*time.Millisecond)
	first, ok := srv.Get("cp1")
	require.True(t, ok)

	c2 := dial()
	defer func() { _ = c2.Close(websocket.StatusNormalClosure, "") }()

	// The replaced peer gets a policy-violation close frame, not an abrupt drop.
	_, _, err := c1.Read(ctx)
	var ce websocket.CloseError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, websocket.StatusPolicyViolation, ce.Code)

	require.Eventually(t, func() bool { return len(snapshot()) == 3 }, 2*time.Second, 10*time.Millisecond)
	// Give the old connection's own goroutine time to (wrongly) notify again.
	time.Sleep(200 * time.Millisecond)

	require.Equal(t, []string{"connect(1)", "disconnect(1)", "connect(2)"}, snapshot())
	mu.Lock()
	require.Len(t, errs, 1)
	require.True(t, errors.Is(errs[0], ocppj.ErrConnReplaced), "got %v", errs[0])
	mu.Unlock()

	cur, ok := srv.Get("cp1")
	require.True(t, ok)
	require.NotSame(t, first, cur)
	mu.Lock()
	require.Same(t, conns[1], cur)
	mu.Unlock()
}
