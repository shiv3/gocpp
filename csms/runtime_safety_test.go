package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

type rsRig struct {
	ctx  context.Context
	srv  *csms.Server
	base string
}

func newRSRig(t *testing.T, opts ...csms.Option) *rsRig {
	t.Helper()
	srv := csms.NewServer(append([]csms.Option{csms.WithSubProtocols("ocpp1.6")}, opts...)...)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return &rsRig{ctx: ctx, srv: srv, base: "ws" + ts.URL[len("http"):] + "/ocpp/"}
}

func (r *rsRig) dial(id string) (*websocket.Conn, *http.Response, error) {
	return websocket.Dial(r.ctx, r.base+id, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
}

func (r *rsRig) mustDial(t *testing.T, id string) *websocket.Conn {
	t.Helper()
	c, _, err := r.dial(id)
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	return c
}

func (r *rsRig) waitConnected(t *testing.T, id string) *csms.Conn {
	t.Helper()
	var c *csms.Conn
	require.Eventually(t, func() bool { var ok bool; c, ok = r.srv.Get(id); return ok }, 2*time.Second, 5*time.Millisecond)
	return c
}

func TestMaxConnections_RefusesWith503AndRetryAfter(t *testing.T) {
	r := newRSRig(t, csms.WithMaxConnections(2))
	a := r.mustDial(t, "a")
	r.mustDial(t, "b")
	r.waitConnected(t, "b")

	_, resp, err := r.dial("c")
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	require.NotEmpty(t, resp.Header.Get("Retry-After"))

	// Same id at the limit replaces the old connection and is not refused.
	r.mustDial(t, "a")
	require.Eventually(t, func() bool {
		_, _, err := a.Read(r.ctx)
		return err != nil
	}, 2*time.Second, 5*time.Millisecond)
	require.ElementsMatch(t, []string{"a", "b"}, r.srv.Connections())

	// A slot frees up when one goes away.
	b, _ := r.srv.Get("b")
	require.NoError(t, b.Close(r.ctx))
	require.Eventually(t, func() bool {
		c, _, err := r.dial("c")
		if err == nil {
			t.Cleanup(func() { _ = c.CloseNow() })
		}
		return err == nil
	}, 2*time.Second, 20*time.Millisecond)
}

func TestMaxConnections_NeverOvershootsUnderRace(t *testing.T) {
	const limit, attempts = 5, 40
	r := newRSRig(t, csms.WithMaxConnections(limit))
	var accepted, refused atomic.Int32
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, resp, err := r.dial("cp" + string(rune('A'+i%26)) + string(rune('a'+i/26)))
			if err != nil {
				require.NotNil(t, resp)
				require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
				refused.Add(1)
				return
			}
			accepted.Add(1)
			t.Cleanup(func() { _ = c.CloseNow() })
		}()
	}
	wg.Wait()
	require.Equal(t, int32(limit), accepted.Load())
	require.Equal(t, int32(attempts-limit), refused.Load())
	require.Eventually(t, func() bool { return len(r.srv.Connections()) == limit }, 2*time.Second, 10*time.Millisecond)
}

func TestMaxMessageSize(t *testing.T) {
	r := newRSRig(t, csms.WithMaxMessageSize(2048))
	ws := r.mustDial(t, "cp1")

	pad := func(n int) string { return repeat('x', n) }
	// Under the limit: answered (NotImplemented, no handler registered).
	require.NoError(t, ws.Write(r.ctx, websocket.MessageText, []byte(`[2,"1","Nope",{"p":"`+pad(500)+`"}]`)))
	_, reply, err := ws.Read(r.ctx)
	require.NoError(t, err)
	require.Contains(t, string(reply), "NotImplemented")

	// Over the limit: the server closes with 1009 Message Too Big.
	_ = ws.Write(r.ctx, websocket.MessageText, []byte(`[2,"2","Nope",{"p":"`+pad(4000)+`"}]`))
	_, _, err = ws.Read(r.ctx)
	require.Equal(t, websocket.StatusMessageTooBig, websocket.CloseStatus(err))
}

func repeat(b byte, n int) string {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return string(out)
}

func TestConnStatsAndConnections(t *testing.T) {
	r := newRSRig(t)
	before := time.Now()
	ws := r.mustDial(t, "cp1")
	c := r.waitConnected(t, "cp1")
	require.Equal(t, []string{"cp1"}, r.srv.Connections())

	st := c.Stats()
	require.False(t, st.ConnectedAt.Before(before.Add(-time.Second)))
	require.Equal(t, st.ConnectedAt, st.LastReceivedAt)
	require.True(t, st.LastSentAt.IsZero())

	time.Sleep(10 * time.Millisecond)
	require.NoError(t, ws.Write(r.ctx, websocket.MessageText, []byte(`[2,"1","Nope",{}]`)))
	_, _, err := ws.Read(r.ctx) // the NotImplemented reply
	require.NoError(t, err)
	st2 := c.Stats()
	require.True(t, st2.LastReceivedAt.After(st.LastReceivedAt))
	require.Equal(t, st2.LastReceivedAt, c.LastActivity())
	require.False(t, st2.LastSentAt.IsZero())

	// PendingCalls: a CALL to the charger stays pending until it answers.
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = csms.CallRaw(r.ctx, c, "Reset", []byte(`{"type":"Soft"}`))
	}()
	_, raw, err := ws.Read(r.ctx)
	require.NoError(t, err)
	require.Equal(t, 1, c.Stats().PendingCalls)
	f, err := ocppj.Parse(raw)
	require.NoError(t, err)
	require.NoError(t, ws.Write(r.ctx, websocket.MessageText, []byte(`[3,"`+f.MsgID+`",{"status":"Accepted"}]`)))
	<-done
	require.Zero(t, c.Stats().PendingCalls)
}

// Handlers keep running (and finish their work) when the charger disconnects;
// ConnFromContext(ctx).Done() tells them it happened.
func TestHandlerContextSurvivesDisconnect(t *testing.T) {
	r := newRSRig(t)
	started, release := make(chan struct{}), make(chan struct{})
	type outcome struct {
		ctxErr       error
		disconnected bool
		sameID       bool
	}
	out := make(chan outcome, 1)
	require.NoError(t, csms.On(r.srv, profiles.StopTransaction, func(ctx context.Context, _ *csms.Conn, _ messages.StopTransactionRequest) (messages.StopTransactionResponse, error) {
		cc, ok := csms.ConnFromContext(ctx)
		require.True(t, ok)
		close(started)
		<-release
		select {
		case <-cc.Done():
			out <- outcome{ctx.Err(), true, cc.ID() == "cp1"}
		default:
			out <- outcome{ctx.Err(), false, false}
		}
		return messages.StopTransactionResponse{}, nil
	}))
	ws := r.mustDial(t, "cp1")
	require.NoError(t, ws.Write(r.ctx, websocket.MessageText,
		[]byte(`[2,"s1","StopTransaction",{"meterStop":1,"timestamp":"2026-01-01T00:00:00Z","transactionId":1}]`)))
	<-started
	require.NoError(t, ws.Close(websocket.StatusNormalClosure, "bye"))
	require.Eventually(t, func() bool { _, ok := r.srv.Get("cp1"); return !ok }, 2*time.Second, 5*time.Millisecond)
	close(release)
	o := <-out
	require.NoError(t, o.ctxErr, "handler ctx must not be cancelled by the disconnect")
	require.True(t, o.disconnected)
	require.True(t, o.sameID)
}

func TestHandlerTimeoutAnswersInternalError(t *testing.T) {
	r := newRSRig(t, csms.WithHandlerTimeout(50*time.Millisecond))
	require.NoError(t, csms.On(r.srv, profiles.StopTransaction, func(ctx context.Context, _ *csms.Conn, _ messages.StopTransactionRequest) (messages.StopTransactionResponse, error) {
		<-ctx.Done()
		return messages.StopTransactionResponse{}, ctx.Err()
	}))
	ws := r.mustDial(t, "cp1")
	require.NoError(t, ws.Write(r.ctx, websocket.MessageText,
		[]byte(`[2,"s1","StopTransaction",{"meterStop":1,"timestamp":"2026-01-01T00:00:00Z","transactionId":1}]`)))
	_, reply, err := ws.Read(r.ctx)
	require.NoError(t, err)
	require.Contains(t, string(reply), `[4,"s1","InternalError"`)
}

func TestConcurrentCallsOptOut(t *testing.T) {
	r := newRSRig(t, csms.WithConcurrentCalls())
	ws := r.mustDial(t, "cp1")
	c := r.waitConnected(t, "cp1")
	for i := 0; i < 2; i++ {
		go func() { _, _ = csms.CallRaw(r.ctx, c, "Reset", []byte(`{"type":"Soft"}`)) }()
	}
	// Both CALLs arrive without any answer in between.
	for i := 0; i < 2; i++ {
		_, _, err := ws.Read(r.ctx)
		require.NoError(t, err)
	}
}

func TestSerializedCallsByDefault(t *testing.T) {
	r := newRSRig(t)
	ws := r.mustDial(t, "cp1")
	c := r.waitConnected(t, "cp1")
	for i := 0; i < 2; i++ {
		go func() { _, _ = csms.CallRaw(r.ctx, c, "Reset", []byte(`{"type":"Soft"}`)) }()
	}
	_, raw, err := ws.Read(r.ctx)
	require.NoError(t, err)
	shortCtx, cancel := context.WithTimeout(r.ctx, 150*time.Millisecond)
	defer cancel()
	_, _, err = ws.Read(shortCtx)
	require.Error(t, err, "second CALL must wait for the first answer")
	_ = raw
}
