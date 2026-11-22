package csms_test

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

type closeRig struct {
	srv    *csms.Server
	ts     *httptest.Server
	url    string
	mu     sync.Mutex
	causes []error
}

func newCloseRig(t *testing.T) *closeRig {
	t.Helper()
	r := &closeRig{}
	r.srv = csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnDisconnect(func(_ *csms.Conn, err error) {
			r.mu.Lock()
			defer r.mu.Unlock()
			r.causes = append(r.causes, err)
		}),
	)
	r.ts = httptest.NewServer(r.srv.Handler())
	t.Cleanup(r.ts.Close)
	r.url = "ws" + r.ts.URL[len("http"):] + "/ocpp/cp1"
	return r
}

func (r *closeRig) dial(ctx context.Context, t *testing.T) *websocket.Conn {
	t.Helper()
	c, _, err := websocket.Dial(ctx, r.url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	return c
}

func (r *closeRig) snapshot() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.causes...)
}

func TestConn_CloseFromServer(t *testing.T) {
	r := newCloseRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c1 := r.dial(ctx, t)
	require.Eventually(t, func() bool { _, ok := r.srv.Get("cp1"); return ok }, 2*time.Second, 10*time.Millisecond)

	// A pending server-initiated call must fail when the connection is closed.
	conn, _ := r.srv.Get("cp1")
	callErr := make(chan error, 1)
	go func() {
		_, err := csms.CallRaw(ctx, conn, "Reset", []byte(`{"type":"Soft"}`))
		callErr <- err
	}()
	_, _, err := c1.Read(ctx) // the CALL arrives; the client never answers
	require.NoError(t, err)

	require.NoError(t, conn.Close(ctx))

	// Teardown is complete when Close returns.
	_, ok := r.srv.Get("cp1")
	require.False(t, ok)
	got := r.snapshot()
	require.Len(t, got, 1)
	require.ErrorIs(t, got[0], ocppj.ErrConnClosedByServer)
	require.ErrorIs(t, <-callErr, ocppj.ErrConnClosedByServer)

	// The client sees a normal closure with the server's reason.
	_, _, err = c1.Read(ctx)
	var ce websocket.CloseError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, websocket.StatusNormalClosure, ce.Code)
	require.Equal(t, "closed by the server", ce.Reason)

	// Closing again is harmless and does not fire the callback twice.
	require.NoError(t, conn.Close(ctx))
	require.Len(t, r.snapshot(), 1)

	// The charge point can reconnect.
	c2 := r.dial(ctx, t)
	defer func() { _ = c2.Close(websocket.StatusNormalClosure, "") }()
	require.Eventually(t, func() bool { _, ok := r.srv.Get("cp1"); return ok }, 2*time.Second, 10*time.Millisecond)
	require.Len(t, r.snapshot(), 1)
}

func TestServer_Disconnect(t *testing.T) {
	r := newCloseRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	require.ErrorIs(t, r.srv.Disconnect(ctx, "cp1"), ocppj.ErrNotConnected)

	c1 := r.dial(ctx, t)
	require.Eventually(t, func() bool { _, ok := r.srv.Get("cp1"); return ok }, 2*time.Second, 10*time.Millisecond)
	require.NoError(t, r.srv.Disconnect(ctx, "cp1"))

	_, _, err := c1.Read(ctx)
	var ce websocket.CloseError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, websocket.StatusNormalClosure, ce.Code)
	got := r.snapshot()
	require.Len(t, got, 1)
	require.ErrorIs(t, got[0], ocppj.ErrConnClosedByServer)
}

func TestConn_CloseWithStatus(t *testing.T) {
	r := newCloseRig(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c1 := r.dial(ctx, t)
	require.Eventually(t, func() bool { _, ok := r.srv.Get("cp1"); return ok }, 2*time.Second, 10*time.Millisecond)

	require.NoError(t, r.srv.DisconnectWithStatus(ctx, "cp1", transport.StatusGoingAway, "maintenance"))

	_, _, err := c1.Read(ctx)
	require.Equal(t, websocket.StatusGoingAway, websocket.CloseStatus(err))
	var ce websocket.CloseError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, "maintenance", ce.Reason)
	got := r.snapshot()
	require.Len(t, got, 1)
	require.ErrorIs(t, got[0], ocppj.ErrConnClosedByServer)

	require.ErrorIs(t, r.srv.DisconnectWithStatus(ctx, "cp1", transport.StatusGoingAway, ""), ocppj.ErrNotConnected)
}

func TestOnAny_CatchAllServesUnregisteredActions(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	var seenAction string
	csms.OnAny(srv, func(ctx context.Context, c *csms.Conn, action string, payload []byte) ([]byte, error) {
		seenAction = action
		return []byte(`{"currentTime":"2020-01-01T00:00:00Z"}`), nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/cp9", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"a1","Whatever",{}]`)))
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(data), `[3,"a1",`)
	require.Equal(t, "Whatever", seenAction)

	csms.OnAny(srv, nil)
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"a2","Whatever",{}]`)))
	_, data, err = c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(data), `[4,"a2","NotImplemented"`)
}
