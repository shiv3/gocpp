package csms_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/shiv3/gocpp/v16"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type gwBoot struct {
	ChargePointVendor string `json:"chargePointVendor"`
	ChargePointModel  string `json:"chargePointModel,omitempty"`
}
type gwBootResp struct {
	Status      string `json:"status"`
	Interval    int    `json:"interval"`
	CurrentTime string `json:"currentTime"`
}

var gwBootMsg = ocppj.Message[gwBoot, gwBootResp]{Action: "BootNotification", Direction: ocppj.SentByCP}

type gwReset struct {
	Type string `json:"type"`
}
type gwResetResp struct {
	Status string `json:"status"`
}

var gwResetMsg = ocppj.Message[gwReset, gwResetResp]{Action: "Reset", Direction: ocppj.SentByCSMS}

// rig is a gateway and a processing tier joined by an in-process hub: two
// separate csms.Server values, the way two processes would be.
type rig struct {
	t   *testing.T
	hub *inproc.Hub
	gw  *csms.Server
	be  *csms.Server
	ts  *httptest.Server
	url string

	stopFn func()
}

func newRig(t *testing.T, gwOpts, beOpts []csms.Option, hubOpts ...inproc.Option) *rig {
	t.Helper()
	r := &rig{t: t, hub: inproc.New(hubOpts...)}
	r.gw = csms.NewGatewayServer(r.hub.Gateway(), append([]csms.Option{csms.WithGatewayID("gw-1")}, gwOpts...)...)
	r.be = csms.NewServer(append([]csms.Option{csms.WithGatewayLink(r.hub.Backend())}, beOpts...)...)
	r.ts = httptest.NewServer(r.gw.Handler())
	r.url = "ws" + r.ts.URL[len("http"):] + "/ocpp/"
	return r
}

// serve starts the backend's ServeGateway loop and returns a stop function that
// drains it.
func (r *rig) serve() (stop func()) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = r.be.ServeGateway(ctx)
	}()
	require.Eventually(r.t, func() bool { return r.hub.Backends() == 1 }, 2*time.Second, 5*time.Millisecond)
	r.stopFn = func() { cancel(); <-done }
	return r.stopFn
}

// close shuts the gateway down first (it still reports the disconnects it
// causes) and the backend last.
func (r *rig) close() {
	r.ts.Close()
	r.gw.Close()
	if r.stopFn != nil {
		r.stopFn()
		r.stopFn = nil
	}
	r.be.Close()
}

func (r *rig) client(opts ...cp.Option) *cp.Client {
	const id = "CP_1"
	c := cp.NewClient(id, r.url+id, append([]cp.Option{cp.WithSubProtocols("ocpp1.6")}, opts...)...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(r.t, c.Connect(ctx))
	r.t.Cleanup(c.Close)
	return c
}

func TestGateway_InboundCallAndCSMSCall(t *testing.T) {
	defer goleak.VerifyNone(t)
	var sess atomic.Value
	r := newRig(t, nil, nil)
	defer r.close()
	require.NoError(t, csms.On(r.be, gwBootMsg, func(ctx context.Context, c *csms.Conn, req gwBoot) (gwBootResp, error) {
		ref, ok := c.GatewaySession()
		require.True(t, ok)
		sess.Store(ref)
		require.Equal(t, "CP_1", c.ID())
		require.Equal(t, ocppj.V16, c.Version())
		require.Equal(t, "ocpp1.6", c.Subprotocol())
		// A handler can call the charger it is serving, through the same handle.
		got, err := csms.Call(ctx, c, gwResetMsg, gwReset{Type: "Soft"})
		require.NoError(t, err)
		require.Equal(t, "Accepted", got.Status)
		return gwBootResp{Status: "Accepted", Interval: 60}, nil
	}))
	r.serve()

	client := r.client()
	require.NoError(t, cp.On(client, gwResetMsg, func(context.Context, gwReset) (gwResetResp, error) {
		return gwResetResp{Status: "Accepted"}, nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cp.Call(ctx, client, gwBootMsg, gwBoot{ChargePointVendor: "Acme"})
	require.NoError(t, err)
	require.Equal(t, 60, resp.Interval)
	ref := sess.Load().(gateway.SessionRef)
	require.Equal(t, "gw-1", ref.GatewayID)
	require.NotZero(t, ref.Epoch)

	// Lookup finds the session through the link and calls work from anywhere.
	h, err := r.be.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	got, err := csms.Call(ctx, h, gwResetMsg, gwReset{Type: "Hard"})
	require.NoError(t, err)
	require.Equal(t, "Accepted", got.Status)
	require.Equal(t, "ocpp1.6", h.Subprotocol())

	_, err = r.be.Lookup(ctx, "NOPE")
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

func TestGateway_ErrorsComeFromBackend(t *testing.T) {
	defer goleak.VerifyNone(t)
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	r := newRig(t, nil, []csms.Option{csms.WithSchemaRegistry(reg), csms.WithStrictSchema(true)})
	defer r.close()
	require.NoError(t, csms.On(r.be, gwBootMsg, func(ctx context.Context, c *csms.Conn, req gwBoot) (gwBootResp, error) {
		if req.ChargePointVendor == "boom" {
			panic("boom")
		}
		if req.ChargePointVendor == "nope" {
			return gwBootResp{}, ocppj.NewCallError(ocppj.ErrorCodeNotSupported, "no thanks", nil)
		}
		return gwBootResp{Status: "Accepted"}, nil
	}))
	r.serve()
	client := r.client()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var ce *ocppj.CallError
	_, err := cp.Call(ctx, client, gwBootMsg, gwBoot{ChargePointVendor: "nope", ChargePointModel: "m"})
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeNotSupported, ce.Code)
	require.Equal(t, "no thanks", ce.Description)

	_, err = cp.Call(ctx, client, gwBootMsg, gwBoot{ChargePointVendor: "boom", ChargePointModel: "m"})
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)

	// Schema validation runs in the backend: the missing model is rejected there.
	_, err = cp.Call(ctx, client, gwBootMsg, gwBoot{ChargePointVendor: "x"})
	require.ErrorAs(t, err, &ce)
	require.Contains(t, string(ce.Code), "Occ")

	// An action with no handler is NotImplemented, decided by the backend.
	_, err = cp.CallRaw(ctx, client, "Heartbeat", []byte(`{}`))
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeNotImplemented, ce.Code)
}

func TestGateway_NoBackendHoldsThenInternalError(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRig(t, nil, nil, inproc.WithUpstreamWait(400*time.Millisecond))
	defer r.close()
	require.NoError(t, csms.On(r.be, gwBootMsg, func(context.Context, *csms.Conn, gwBoot) (gwBootResp, error) {
		return gwBootResp{Status: "Accepted"}, nil
	}))
	// No backend attached at connect time: the upgrade itself needs a decision.
	_, _, err := websocket.Dial(context.Background(), r.url+"CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.Error(t, err, "no backend, no connect decision")

	stop := r.serve()
	client := r.client()
	stop() // backend leaves; the charger stays connected
	require.Equal(t, 0, r.hub.Backends())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	var ce *ocppj.CallError
	_, err = cp.Call(ctx, client, gwBootMsg, gwBoot{ChargePointVendor: "x"})
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)
	require.GreaterOrEqual(t, time.Since(start), 350*time.Millisecond, "held for a backend")
	require.Less(t, time.Since(start), 3*time.Second)

	// A backend that attaches while the call is held gets it.
	go func() {
		time.Sleep(100 * time.Millisecond)
		r.hub.AttachBackend(backendOf(r.be), gateway.BackendInfo{})
	}()
	resp, err := cp.Call(ctx, client, gwBootMsg, gwBoot{ChargePointVendor: "x"})
	require.NoError(t, err)
	require.Equal(t, "Accepted", resp.Status)
}

// backendOf exposes a Server's Upstream for tests that attach it by hand.
func backendOf(s *csms.Server) gateway.Upstream { return s.Upstream() }

func TestGateway_SessionEventsAndEpochs(t *testing.T) {
	defer goleak.VerifyNone(t)
	type ev struct {
		kind  string
		epoch uint64
		err   error
	}
	var mu sync.Mutex
	var evs []ev
	rec := func(e ev) { mu.Lock(); evs = append(evs, e); mu.Unlock() }
	r := newRig(t, nil, []csms.Option{
		csms.WithOnConnect(func(c *csms.Conn) {
			ref, _ := c.GatewaySession()
			rec(ev{kind: "connect", epoch: ref.Epoch})
		}),
		csms.WithOnDisconnect(func(c *csms.Conn, err error) {
			ref, _ := c.GatewaySession()
			rec(ev{kind: "disconnect", epoch: ref.Epoch, err: err})
		}),
	})
	defer r.close()
	r.serve()

	c1 := r.client()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(evs) == 1 }, 2*time.Second, 5*time.Millisecond)

	// A second connection with the same id replaces the first: the backend sees
	// the old session go before the new one comes up, with a distinguishing cause.
	c2 := r.client()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(evs) == 3 }, 2*time.Second, 5*time.Millisecond)
	mu.Lock()
	require.Equal(t, "connect", evs[0].kind)
	require.Equal(t, "disconnect", evs[1].kind)
	require.ErrorIs(t, evs[1].err, ocppj.ErrConnReplaced)
	require.Equal(t, "connect", evs[2].kind)
	require.Greater(t, evs[2].epoch, evs[0].epoch)
	require.Equal(t, evs[0].epoch, evs[1].epoch)
	mu.Unlock()
	_ = c1

	// The late SessionClosed of the replaced socket must not produce a second
	// disconnect, and closing the live one produces exactly one.
	c2.Close()
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(evs) == 4 }, 2*time.Second, 5*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	require.Len(t, evs, 4)
	require.Equal(t, "disconnect", evs[3].kind)
	mu.Unlock()
}

func TestGateway_CloseFromBackend(t *testing.T) {
	defer goleak.VerifyNone(t)
	disc := make(chan error, 1)
	r := newRig(t, nil, []csms.Option{csms.WithOnDisconnect(func(_ *csms.Conn, err error) { disc <- err })})
	defer r.close()
	r.serve()
	_ = r.client()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h *csms.Conn
	require.Eventually(t, func() bool {
		var err error
		h, err = r.be.Lookup(ctx, "CP_1")
		return err == nil
	}, 2*time.Second, 5*time.Millisecond)
	require.NoError(t, h.Close(ctx))
	select {
	case err := <-disc:
		require.ErrorIs(t, err, ocppj.ErrConnClosedByServer)
	case <-time.After(3 * time.Second):
		t.Fatal("no disconnect event")
	}
}

func TestGateway_StaleEpochAndSentClassification(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRig(t, []csms.Option{csms.WithCallTimeout(300 * time.Millisecond)}, nil)
	defer r.close()
	r.serve()
	client := r.client()
	release := make(chan struct{})
	require.NoError(t, cp.On(client, gwResetMsg, func(ctx context.Context, _ gwReset) (gwResetResp, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return gwResetResp{Status: "Accepted"}, nil
	}))
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h *csms.Conn
	require.Eventually(t, func() bool {
		var err error
		h, err = r.be.Lookup(ctx, "CP_1")
		return err == nil
	}, 2*time.Second, 5*time.Millisecond)
	ref, _ := h.GatewaySession()

	// A wrong epoch is fenced at the gateway and nothing is sent.
	dn := r.hub.Backend()
	rep, err := dn.Call(ctx, gateway.CallRequest{Target: gateway.SessionRef{CPID: "CP_1", GatewayID: "gw-1", Epoch: ref.Epoch + 7},
		Action: "Reset", Payload: []byte(`{"type":"Soft"}`)})
	require.NoError(t, err)
	require.ErrorIs(t, rep.Error.Err(), gateway.ErrSessionStale)
	require.Equal(t, gateway.NotSent, rep.Sent)

	// The charger holds the call past the call timeout: written, no answer.
	_, err = csms.Call(ctx, h, gwResetMsg, gwReset{Type: "Soft"})
	require.ErrorIs(t, err, ocppj.ErrCallTimeout)
	require.Equal(t, gateway.SentNoResponse, gateway.SentOf(err))
}

func TestGateway_OrphanResultAndCallIDDedupe(t *testing.T) {
	defer goleak.VerifyNone(t)
	type orphan struct {
		callID, action string
		payload        []byte
		err            error
	}
	orphans := make(chan orphan, 2)
	r := newRig(t, nil, []csms.Option{csms.WithOnOrphanResult(func(_, callID, action string, payload []byte, err error) {
		orphans <- orphan{callID, action, payload, err}
	})})
	defer r.close()
	r.serve()
	client := r.client()
	var received atomic.Int32
	gate := make(chan struct{})
	require.NoError(t, cp.On(client, gwResetMsg, func(context.Context, gwReset) (gwResetResp, error) {
		received.Add(1)
		<-gate
		return gwResetResp{Status: "Accepted"}, nil
	}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var h *csms.Conn
	require.Eventually(t, func() bool {
		var err error
		h, err = r.be.Lookup(ctx, "CP_1")
		return err == nil
	}, 2*time.Second, 5*time.Millisecond)
	ref, _ := h.GatewaySession()
	dn := r.hub.Backend()

	// Two requests with one CallID share one outcome: the charger sees one CALL.
	req := gateway.CallRequest{Target: ref, CallID: "c-1", Action: "Reset", Payload: []byte(`{"type":"Soft"}`)}
	var wg sync.WaitGroup
	replies := make([]gateway.CallReply, 2)
	cctx, ccancel := context.WithCancel(ctx)
	for i := range replies {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := ctx
			if i == 0 {
				c = cctx // the first caller will go away after the frame is written
			}
			replies[i], _ = dn.Call(c, req)
		}()
	}
	require.Eventually(t, func() bool { return received.Load() == 1 }, 2*time.Second, 5*time.Millisecond)
	ccancel() // caller (stream) gone, frame already written
	time.Sleep(50 * time.Millisecond)
	close(gate) // the charger answers late
	wg.Wait()
	require.Equal(t, int32(1), received.Load(), "deduped by CallID")

	// Exactly one of the two callers (the one whose context was cancelled) gets
	// the "gone" outcome; the other shares the real answer or the orphan path
	// reports it. Either way the answer is not lost.
	var answered, orphaned int
	for _, rp := range replies {
		if rp.Sent == gateway.Answered {
			answered++
		}
	}
	select {
	case o := <-orphans:
		orphaned++
		require.Equal(t, "c-1", o.callID)
		require.Equal(t, "Reset", o.action)
		require.JSONEq(t, `{"status":"Accepted"}`, string(o.payload))
	case <-time.After(500 * time.Millisecond):
	}
	require.GreaterOrEqual(t, answered+orphaned, 1, "the charger's answer reached someone")
}

func TestGateway_AuthAndConnConfigDecidedByBackend(t *testing.T) {
	defer goleak.VerifyNone(t)
	var seenTimeouts atomic.Value
	r := newRig(t, nil, []csms.Option{
		csms.WithAuthenticator(auth.BasicAuth(func(cpID, pw string) (auth.Identity, error) {
			if pw != "secret" {
				return auth.Identity{}, auth.ErrUnauthorized
			}
			return auth.Identity{CPID: cpID}, nil
		})),
		csms.WithConnConfig(func(_ *http.Request, cpID string, tm *csms.ConnTimeouts) {
			tm.PingInterval = 0
			seenTimeouts.Store(cpID)
		}),
	})
	defer r.close()
	r.serve()

	_, resp, err := websocket.Dial(context.Background(), r.url+"CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.Error(t, err)
	require.Equal(t, http.StatusUnauthorized, resp.StatusCode)

	h := http.Header{}
	h.Set("Authorization", "Basic "+basic("CP_1", "secret"))
	c, _, err := websocket.Dial(context.Background(), r.url+"CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, HTTPHeader: h})
	require.NoError(t, err)
	_ = c.CloseNow()
	require.Equal(t, "CP_1", seenTimeouts.Load())
}

func basic(u, p string) string {
	return base64.StdEncoding.EncodeToString([]byte(u + ":" + p))
}

// frames runs a deterministic exchange against ts through a raw WebSocket and
// returns every frame the server wrote, in order.
func frames(t *testing.T, newServer func(csms.Option) (http.Handler, func())) []string {
	t.Helper()
	var mu sync.Mutex
	var out []string
	obs := csms.WithFrameObserver(func(_ context.Context, ev observability.FrameEvent) {
		if ev.Direction == observability.FrameOut {
			mu.Lock()
			out = append(out, string(ev.Raw))
			mu.Unlock()
		}
	})
	h, closeFn := newServer(obs)
	defer closeFn()
	ts := httptest.NewServer(h)
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()
	for _, f := range []string{
		`[2,"a1","BootNotification",{"chargePointVendor":"V","chargePointModel":"M"}]`,
		`[2,"a2","BootNotification",{"chargePointVendor":"V"}]`,                           // schema violation
		`[2,"a3","BootNotification",{"chargePointVendor":"boom","chargePointModel":"M"}]`, // handler panic
		`[2,"a4","BootNotification",{"chargePointVendor":"nope","chargePointModel":"M"}]`, // handler CallError
		`[2,"a5","Unknown",{}]`, // not implemented
		`[2,"a6","BootNotification",{"chargePointVendor":"V","chargePointModel":"M","x":1}]`, // additional property
		`[2,"a7","BootNotification",{"chargePointVendor":5,"chargePointModel":"M"}]`,         // type
	} {
		require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(f)))
		_, _, err := c.Read(ctx)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(out) == 7 }, 2*time.Second, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), out...)
}

// The same deterministic exchange gives byte-identical frames from an embedded
// server and from a gateway with a backend behind it.
func TestGateway_WireIdenticalToEmbedded(t *testing.T) {
	defer goleak.VerifyNone(t)
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	handler := func(ctx context.Context, c *csms.Conn, req gwBoot) (gwBootResp, error) {
		switch req.ChargePointVendor {
		case "boom":
			panic("boom")
		case "nope":
			return gwBootResp{}, ocppj.NewCallError(ocppj.ErrorCodeNotSupported, "no thanks", map[string]any{"k": "v"})
		}
		return gwBootResp{Status: "Accepted", Interval: 300, CurrentTime: "2026-06-15T00:00:00Z"}, nil
	}
	common := []csms.Option{csms.WithSchemaRegistry(reg), csms.WithStrictSchema(true)}

	embedded := frames(t, func(obs csms.Option) (http.Handler, func()) {
		s := csms.NewServer(append(common, obs)...)
		require.NoError(t, csms.On(s, gwBootMsg, handler))
		return s.Handler(), s.Close
	})
	split := frames(t, func(obs csms.Option) (http.Handler, func()) {
		hub := inproc.New()
		gw := csms.NewGatewayServer(hub.Gateway(), append(common, obs)...)
		be := csms.NewServer(append(common, csms.WithGatewayLink(hub.Backend()))...)
		require.NoError(t, csms.On(be, gwBootMsg, handler))
		stop := hub.AttachBackend(be.Upstream(), gateway.BackendInfo{})
		return gw.Handler(), func() {
			gw.Close()
			be.Close()
			_ = stop(context.Background())
		}
	})
	require.Len(t, embedded, 7)
	require.Equal(t, embedded, split)
	for _, f := range split {
		var arr []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(f), &arr))
	}
}

// Two backend pools with a router that alternates between them (a stand-in for a
// 50% policy): the frames written to the charger are still byte-identical to
// embedded mode, and both pools served.
func TestGateway_TwoPoolsWireIdentical(t *testing.T) {
	defer goleak.VerifyNone(t)
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	handler := func(ctx context.Context, c *csms.Conn, req gwBoot) (gwBootResp, error) {
		switch req.ChargePointVendor {
		case "boom":
			panic("boom")
		case "nope":
			return gwBootResp{}, ocppj.NewCallError(ocppj.ErrorCodeNotSupported, "no thanks", map[string]any{"k": "v"})
		}
		return gwBootResp{Status: "Accepted", Interval: 300, CurrentTime: "2026-06-15T00:00:00Z"}, nil
	}
	common := []csms.Option{csms.WithSchemaRegistry(reg), csms.WithStrictSchema(true)}
	embedded := frames(t, func(obs csms.Option) (http.Handler, func()) {
		s := csms.NewServer(append(common, obs)...)
		require.NoError(t, csms.On(s, gwBootMsg, handler))
		return s.Handler(), s.Close
	})
	var served [2]atomic.Int64
	var n atomic.Int64
	split := frames(t, func(obs csms.Option) (http.Handler, func()) {
		hub := inproc.New()
		router := gateway.RouterFunc(func(in gateway.RouteInput) gateway.RouteDecision {
			if in.Action == "$connect" || in.Action == "$opened" || in.Action == "$closed" {
				return gateway.RouteDecision{Pool: gateway.DefaultPool}
			}
			if n.Add(1)%2 == 0 {
				return gateway.RouteDecision{Pool: "canary", Rule: "alt", Fallback: gateway.DefaultPool}
			}
			return gateway.RouteDecision{Pool: gateway.DefaultPool}
		})
		gw := csms.NewGatewayServer(hub.Gateway(), append(common, obs, csms.WithRouter(router))...)
		var stops []func(context.Context) error
		var bes []*csms.Server
		for i, pool := range []string{"", "canary"} {
			be := csms.NewServer(append(common, csms.WithGatewayLink(hub.Backend()), csms.WithBackendInfo(gateway.BackendInfo{Pool: pool, Version: "v" + pool}))...)
			require.NoError(t, csms.On(be, gwBootMsg, func(ctx context.Context, c *csms.Conn, req gwBoot) (gwBootResp, error) {
				served[i].Add(1)
				return handler(ctx, c, req)
			}))
			up := be.Upstream()
			stops = append(stops, hub.AttachBackend(up, gateway.BackendInfo{Pool: pool, Caps: []string{gateway.CapSend21}}))
			bes = append(bes, be)
		}
		return gw.Handler(), func() {
			gw.Close()
			for _, s := range stops {
				_ = s(context.Background())
			}
			for _, b := range bes {
				b.Close()
			}
		}
	})
	require.Equal(t, embedded, split)
	require.NotZero(t, served[0].Load())
	require.NotZero(t, served[1].Load(), "the canary pool served too")
}

// Without a router a backend that attaches in another pool receives nothing:
// starting a canary is not a traffic change by itself.
func TestGateway_CanaryPoolGetsNothingWithoutRouter(t *testing.T) {
	defer goleak.VerifyNone(t)
	hub := inproc.New()
	gw := csms.NewGatewayServer(hub.Gateway(), csms.WithGatewayID("gw-1"))
	var stable, canary atomic.Int64
	mk := func(pool string, ctr *atomic.Int64) (*csms.Server, func(context.Context) error) {
		be := csms.NewServer(csms.WithGatewayLink(hub.Backend()))
		require.NoError(t, csms.On(be, gwBootMsg, func(context.Context, *csms.Conn, gwBoot) (gwBootResp, error) {
			ctr.Add(1)
			return gwBootResp{Status: "Accepted"}, nil
		}))
		return be, hub.AttachBackend(be.Upstream(), gateway.BackendInfo{Pool: pool, Version: pool})
	}
	b1, d1 := mk("", &stable)
	b2, d2 := mk("canary", &canary)
	ts := httptest.NewServer(gw.Handler())
	defer func() {
		ts.Close()
		gw.Close()
		_ = d1(context.Background())
		_ = d2(context.Background())
		b1.Close()
		b2.Close()
	}()
	c := cp.NewClient("CP_1", "ws"+ts.URL[4:]+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	defer c.Close()
	for i := 0; i < 20; i++ {
		_, err := cp.Call(ctx, c, gwBootMsg, gwBoot{ChargePointVendor: "x"})
		require.NoError(t, err)
	}
	require.EqualValues(t, 20, stable.Load())
	require.EqualValues(t, 0, canary.Load())
	require.Len(t, hub.Pools(), 2)
}

// SetAttrs from the backend changes what the router sees from the next message,
// without touching the connection; a replace clears, an empty value deletes.
func TestGateway_SetAttrsDrivesRouting(t *testing.T) {
	defer goleak.VerifyNone(t)
	hub := inproc.New()
	var seen sync.Map
	router := gateway.RouterFunc(func(in gateway.RouteInput) gateway.RouteDecision {
		seen.Store(in.Action, in.Attrs["label.canary"])
		return gateway.RouteDecision{Pool: gateway.DefaultPool}
	})
	gw := csms.NewGatewayServer(hub.Gateway(), csms.WithRouter(router))
	be := csms.NewServer(csms.WithGatewayLink(hub.Backend()),
		csms.WithAuthenticator(auth.None{}))
	require.NoError(t, csms.On(be, gwBootMsg, func(ctx context.Context, c *csms.Conn, req gwBoot) (gwBootResp, error) {
		require.NoError(t, c.SetAttrs(ctx, map[string]string{"label.canary": "1"}))
		return gwBootResp{Status: "Accepted"}, nil
	}))
	drain := hub.AttachBackend(be.Upstream(), gateway.BackendInfo{})
	ts := httptest.NewServer(gw.Handler())
	defer func() { ts.Close(); gw.Close(); _ = drain(context.Background()); be.Close() }()
	c := cp.NewClient("CP_1", "ws"+ts.URL[4:]+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	defer c.Close()
	_, err := cp.Call(ctx, c, gwBootMsg, gwBoot{ChargePointVendor: "x"})
	require.NoError(t, err)
	_, err = cp.Call(ctx, c, gwBootMsg, gwBoot{ChargePointVendor: "x"})
	require.NoError(t, err)
	// The router saw the label on a message after SetAttrs.
	v, _ := seen.Load("BootNotification")
	require.Equal(t, "1", v)

	// Too much attribute data is refused; a replace clears.
	h, err := be.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	big := map[string]string{"k": string(make([]byte, 2000))}
	require.ErrorIs(t, h.SetAttrs(ctx, big), csms.ErrAttrsTooLarge)
	require.NoError(t, h.SetAttrs(ctx, map[string]string{"a": "b"}))
	require.NoError(t, h.SetAttrs(ctx, map[string]string{"a": "", "label.canary": ""}))
	_, err = cp.Call(ctx, c, gwBootMsg, gwBoot{ChargePointVendor: "x"})
	require.NoError(t, err)
	v, _ = seen.Load("BootNotification")
	require.Equal(t, "", v)
}
