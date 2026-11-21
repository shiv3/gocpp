package csms_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"sync/atomic"
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
	id  string
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
	return &node{id: id, srv: srv, ts: ts, reg: reg}
}

// connect dials the charge point to this node. reply answers each CALL the CSMS
// sends: nil answers with Accepted, a CallError answers with that error.
func (n *node) connect(ctx context.Context, t *testing.T, cpID string, reply func(action string) (callError *ocppj.CallError)) *websocket.Conn {
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
	// The global route is published just after the local connection becomes
	// visible, so wait for both. The route must name THIS node: when the charge
	// point moves between nodes, the previous owner's entry is still present
	// until this node's publish lands, and accepting it made callers race with
	// that publish.
	require.Eventually(t, func() bool {
		_, local := n.srv.Get(cpID)
		inst, global, _ := n.reg.LookupGlobal(ctx, cpID)
		return local && global && inst == n.id
	}, 2*time.Second, 10*time.Millisecond)
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
	a.connect(ctx, t, "CP_1", func(string) *ocppj.CallError { return nil })

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
	a.connect(ctx, t, "CP_1", func(string) *ocppj.CallError {
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
	cp := a.connect(ctx, t, "CP_1", func(string) *ocppj.CallError { return nil })
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
	staleCP := a.connect(ctx, t, "CP_1", func(string) *ocppj.CallError { return nil })

	// The charger reconnects to B while A still holds its (now stale) entry.
	b.connect(ctx, t, "CP_1", func(string) *ocppj.CallError { return nil })
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
	a := &node{id: "A", srv: srv, ts: ts, reg: reg}
	cp := a.connect(ctx, t, "CP_1", func(string) *ocppj.CallError { return nil })

	hub.Bind("CP_1", "B") // another instance took over
	_ = cp.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok := a.srv.Get("CP_1"); return !ok }, 2*time.Second, 10*time.Millisecond)
	time.Sleep(50 * time.Millisecond)
	inst, ok := owner(t, a, "CP_1")
	require.True(t, ok)
	require.Equal(t, "B", inst)

	// Without a takeover the entry is removed.
	cp2 := a.connect(ctx, t, "CP_2", func(string) *ocppj.CallError { return nil })
	_, ok = owner(t, a, "CP_2")
	require.True(t, ok)
	_ = cp2.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool { _, ok := owner(t, a, "CP_2"); return !ok }, 2*time.Second, 10*time.Millisecond)
}

// gatedRegistry blocks PutGlobal until release is closed, to force a slow
// registry write.
type gatedRegistry struct {
	storage.ConnectionRegistry
	release chan struct{}
}

func (g gatedRegistry) PutGlobal(ctx context.Context, cpID, inst string) error {
	select {
	case <-g.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return g.ConnectionRegistry.PutGlobal(ctx, cpID, inst)
}

// The session is claimed before the upgrade is accepted: a slow registry write
// delays the handshake itself, so the connection never looks established (and
// no inbound handler runs) before its route exists.
func TestCluster_RouteClaimedBeforeUpgradeAccepted(t *testing.T) {
	hub := memory.NewInProcessHub()
	reg := gatedRegistry{hub.Registry(), make(chan struct{})}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInstanceID("A"),
		csms.WithConnectionRegistry(reg), csms.WithMessageRouter(hub.Router("A")))
	var routeAtHandler atomic.Value
	msg := ocppj.Message[anyReqT, anyReqT]{Action: "Heartbeat", Direction: ocppj.SentByCP}
	require.NoError(t, csms.On(srv, msg, func(ctx context.Context, c *csms.Conn, _ anyReqT) (anyReqT, error) {
		inst, _, _ := reg.LookupGlobal(ctx, c.ID())
		routeAtHandler.Store(inst)
		return anyReqT{}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dialed := make(chan *websocket.Conn, 1)
	go func() {
		c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		if err == nil {
			dialed <- c
		}
	}()
	select {
	case <-dialed:
		t.Fatal("upgrade accepted while the claim was pending")
	case <-time.After(150 * time.Millisecond):
	}
	_, connected := srv.Get("CP_1")
	require.False(t, connected)
	close(reg.release)
	c := <-dialed
	t.Cleanup(func() { _ = c.CloseNow() })
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"1","Heartbeat",{}]`)))
	_, reply, err := c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(reply), `[3,"1"`)
	require.Equal(t, "A", routeAtHandler.Load())
}
