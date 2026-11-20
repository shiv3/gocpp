package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// After the upgrade the HTTP handler returns and the connection is torn down
// from a context.AfterFunc; nothing may keep the *http.Request (headers, TLS
// state, body) alive for the lifetime of the connection.
func TestServer_RequestCollectableAfterUpgrade(t *testing.T) {
	released := make(chan struct{})
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithConnConfig(func(r *http.Request, _ string, _ *csms.ConnTimeouts) {
			runtime.SetFinalizer(r, func(*http.Request) { close(released) })
		}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP1",
		&websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()
	go func() { _, _, _ = c.Read(ctx) }()
	require.Eventually(t, func() bool { _, ok := srv.Get("CP1"); return ok }, 2*time.Second, 10*time.Millisecond)

	require.Eventually(t, func() bool {
		runtime.GC()
		select {
		case <-released:
			return true
		default:
			return false
		}
	}, 10*time.Second, 50*time.Millisecond, "the upgrade request is still referenced while the connection is open")
	_, ok := srv.Get("CP1")
	require.True(t, ok, "connection must still be registered")
}

// The HTTP serve goroutine must not stay parked per connection: after upgrade a
// connection costs its own goroutines only (reader, writer, dispatcher).
func TestServer_NoServeGoroutinePerConnection(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	const n = 30
	runtime.GC()
	time.Sleep(100 * time.Millisecond)
	before := runtime.NumGoroutine()
	for i := 0; i < n; i++ {
		c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/"+string(rune('A'+i))+"X",
			&websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		require.NoError(t, err)
		defer func() { _ = c.CloseNow() }()
	}
	require.Eventually(t, func() bool { return len(srv.Connections()) == n }, 5*time.Second, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	// Server side: reader, writer and dispatcher per connection (the client side
	// keeps none), plus a little slack for runtime helpers.
	grew := runtime.NumGoroutine() - before
	require.LessOrEqual(t, grew, n*3+5, "goroutines per connection: %.1f", float64(grew)/n)
}

// Shutdown leaves nothing behind: every cleanup callback has run and closed gone.
func TestServer_ShutdownCleansUpAllConnections(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreCurrent())
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, id := range []string{"A", "B", "C"} {
		c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/"+id,
			&websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		require.NoError(t, err)
		go func() { _, _, _ = c.Read(ctx) }()
	}
	require.Eventually(t, func() bool { return len(srv.Connections()) == 3 }, 5*time.Second, 10*time.Millisecond)
	require.NoError(t, srv.Shutdown(ctx))
	require.Empty(t, srv.Connections())
	ts.Close()
}
