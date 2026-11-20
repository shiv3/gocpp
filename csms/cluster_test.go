package csms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

type node struct {
	srv *csms.Server
	ts  *httptest.Server
	reg storage.ConnectionRegistry
}

func newNode(t *testing.T, hub *memory.InProcessHub, id string) *node {
	t.Helper()
	reg := hub.Registry()
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithInstanceID(id),
		csms.WithConnectionRegistry(reg),
		csms.WithMessageRouter(hub.Router(id)),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return &node{srv: srv, ts: ts, reg: reg}
}

// connect dials the charge point to this node. reply answers each CALL the CSMS
// sends; it gets the action and returns the raw frame tail after the message id
// (e.g. `3,{"status":"Accepted"}` is built by the helper below).
func (n *node) connect(t *testing.T, ctx context.Context, cpID string, reply func(action string) (callError *ocppj.CallError)) *websocket.Conn {
	t.Helper()
	url := "ws" + n.ts.URL[len("http"):] + "/ocpp/" + cpID
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = c.CloseNow() })
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			var frame []json.RawMessage
			if json.Unmarshal(data, &frame) != nil || len(frame) < 3 {
				continue
			}
			var id, action string
			_ = json.Unmarshal(frame[1], &id)
			_ = json.Unmarshal(frame[2], &action)
			var out []byte
			if ce := reply(action); ce != nil {
				out, _ = json.Marshal([]any{4, id, string(ce.Code), ce.Description, map[string]any{}})
			} else {
				out, _ = json.Marshal([]any{3, id, map[string]any{"status": "Accepted"}})
			}
			_ = c.Write(ctx, websocket.MessageText, out)
		}
	}()
	require.Eventually(t, func() bool { _, ok := n.srv.Get(cpID); return ok }, 2*time.Second, 10*time.Millisecond)
	return c
}

func owner(t *testing.T, n *node, cpID string) (string, bool) {
	t.Helper()
	inst, ok, err := n.reg.LookupGlobal(context.Background(), cpID)
	require.NoError(t, err)
	return inst, ok
}

func TestCluster_CallForwardedToOwningInstance(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	a, b := newNode(t, hub, "A"), newNode(t, hub, "B")
	a.connect(t, ctx, "CP_1", func(string) *ocppj.CallError { return nil })

	inst, ok := owner(t, b, "CP_1")
	require.True(t, ok)
	require.Equal(t, "A", inst)

	conn, err := b.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, conn.Remote())
	require.Equal(t, "CP_1", conn.ID())

	resp, err := csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.NoError(t, err)
	require.Equal(t, messages.ResetResponseStatusAccepted, resp.Status)

	raw, err := csms.CallRaw(ctx, conn, "Reset", []byte(`{"type":"Hard"}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"Accepted"}`, string(raw))

	type result struct {
		resp messages.ResetResponse
		err  error
	}
	ch := make(chan result, 1)
	require.NoError(t, csms.CallAsync(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft},
		func(r messages.ResetResponse, err error) { ch <- result{r, err} }))
	got := <-ch
	require.NoError(t, got.err)
	require.Equal(t, messages.ResetResponseStatusAccepted, got.resp.Status)

	// Lookup on the owner itself yields the live local connection.
	local, err := a.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.False(t, local.Remote())

	require.ErrorIs(t, conn.Close(ctx), csms.ErrRemoteConn)
}

func TestCluster_CallErrorPropagates(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	a, b := newNode(t, hub, "A"), newNode(t, hub, "B")
	a.connect(t, ctx, "CP_1", func(string) *ocppj.CallError {
		return ocppj.NewCallError(ocppj.ErrorCodeNotSupported, "no reset here", nil)
	})
	conn, err := b.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)

	_, err = csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	var ce *ocppj.CallError
	require.True(t, errors.As(err, &ce), "got %v", err)
	require.Equal(t, ocppj.ErrorCodeNotSupported, ce.Code)
	require.Equal(t, "no reset here", ce.Description)
}

func TestCluster_UnknownChargePoint(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	b := newNode(t, hub, "B")
	_, err := b.srv.Lookup(ctx, "NOPE")
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

func TestCluster_OwnerLostConnectionAfterLookup(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	a, b := newNode(t, hub, "A"), newNode(t, hub, "B")
	cp := a.connect(t, ctx, "CP_1", func(string) *ocppj.CallError { return nil })
	conn, err := b.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)

	_ = cp.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok := owner(t, b, "CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)

	_, err = csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
	_, err = b.srv.Lookup(ctx, "CP_1")
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

func TestCluster_StaleDisconnectKeepsNewerOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	a, b := newNode(t, hub, "A"), newNode(t, hub, "B")
	staleCP := a.connect(t, ctx, "CP_1", func(string) *ocppj.CallError { return nil })

	// The charger reconnects to B while A still holds its (now stale) entry.
	b.connect(t, ctx, "CP_1", func(string) *ocppj.CallError { return nil })
	inst, ok := owner(t, a, "CP_1")
	require.True(t, ok)
	require.Equal(t, "B", inst, "last registration wins")

	// A's stale connection goes away later; B's entry must survive.
	_ = staleCP.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok := a.srv.Get("CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond) // let A's deferred route removal run
	inst, ok = owner(t, a, "CP_1")
	require.True(t, ok)
	require.Equal(t, "B", inst)

	// And a call from A still reaches the charger on B.
	conn, err := a.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	require.True(t, conn.Remote())
	resp, err := csms.Call(ctx, conn, profiles.Reset, messages.ResetRequest{Type: messages.ResetRequestTypeSoft})
	require.NoError(t, err)
	require.Equal(t, messages.ResetResponseStatusAccepted, resp.Status)
}

func TestCluster_SingleInstanceKeepsLocalBehaviour(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	t.Cleanup(srv.Close)
	_, err := srv.Lookup(context.Background(), "CP_1")
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

// plainRegistry hides the optional OwnedGlobalDeleter of the wrapped registry.
type plainRegistry struct{ storage.ConnectionRegistry }

func TestCluster_StaleDisconnectWithoutCompareAndDelete(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	hub := memory.NewInProcessHub()
	reg := plainRegistry{hub.Registry()}
	_, isCAD := storage.ConnectionRegistry(reg).(storage.OwnedGlobalDeleter)
	require.False(t, isCAD)
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInstanceID("A"),
		csms.WithConnectionRegistry(reg), csms.WithMessageRouter(hub.Router("A")))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	a := &node{srv: srv, ts: ts, reg: reg}
	cp := a.connect(t, ctx, "CP_1", func(string) *ocppj.CallError { return nil })

	hub.Bind("CP_1", "B") // another instance took over
	_ = cp.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok := a.srv.Get("CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	inst, ok := owner(t, a, "CP_1")
	require.True(t, ok)
	require.Equal(t, "B", inst)

	// Without a takeover the entry is removed.
	cp2 := a.connect(t, ctx, "CP_2", func(string) *ocppj.CallError { return nil })
	_, ok = owner(t, a, "CP_2")
	require.True(t, ok)
	_ = cp2.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok := owner(t, a, "CP_2"); return !ok }, 2*time.Second, 10*time.Millisecond)
}
