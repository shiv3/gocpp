package linkgrpc_test

import (
	"context"
	"net"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type bootReq struct {
	ChargePointVendor string `json:"chargePointVendor"`
}
type bootResp struct {
	Status      string `json:"status"`
	Interval    int    `json:"interval"`
	CurrentTime string `json:"currentTime"`
}
type resetReq struct {
	Type string `json:"type"`
}
type resetResp struct {
	Status string `json:"status"`
}

var (
	bootMsg  = ocppj.Message[bootReq, bootResp]{Action: "BootNotification", Direction: ocppj.SentByCP}
	resetMsg = ocppj.Message[resetReq, resetResp]{Action: "Reset", Direction: ocppj.SentByCSMS}
)

// topology is one gateway (csms.Server + gRPC listener) and any number of
// backends, all in this process but talking real gRPC over loopback.
type topology struct {
	t     *testing.T
	reg   storage.SessionRegistry
	link  *linkgrpc.Gateway
	gw    *csms.Server
	gs    *grpc.Server
	addr  string
	ts    *httptest.Server
	wsURL string
	nodes []*backendNode
}

func newTopology(t *testing.T, gwOpts ...linkgrpc.GatewayOption) *topology {
	t.Helper()
	return newTopologyWith(t, nil, gwOpts...)
}

func newTopologyWith(t *testing.T, csmsOpts []csms.Option, gwOpts ...linkgrpc.GatewayOption) *topology {
	t.Helper()
	tp := &topology{t: t, reg: memory.NewSessionRegistry()}
	tp.link = linkgrpc.NewGateway(gwOpts...)
	tp.gw = csms.NewGatewayServer(tp.link, append([]csms.Option{csms.WithGatewayID("gw-1"), csms.WithSessionRegistry(tp.reg)}, csmsOpts...)...)
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	tp.gs = grpc.NewServer()
	tp.link.Register(tp.gs)
	go func() { _ = tp.gs.Serve(lis) }()
	tp.addr = lis.Addr().String()
	tp.ts = httptest.NewServer(tp.gw.Handler())
	tp.wsURL = "ws" + tp.ts.URL[4:] + "/ocpp/"
	return tp
}

type backendNode struct {
	srv  *csms.Server
	stop context.CancelFunc
	done chan struct{}
	err  error
	once sync.Once
}

func (tp *topology) addBackend(info gateway.BackendInfo, handler func(context.Context, *csms.Conn, bootReq) (bootResp, error)) *backendNode {
	tp.t.Helper()
	return tp.addBackendWith(info, nil, []csms.Option{csms.WithSessionRegistry(tp.reg)}, handler)
}

// addBackendWith is addBackend with extra link options and server options (the
// session registry is not implied).
func (tp *topology) addBackendWith(info gateway.BackendInfo, beOpts []linkgrpc.BackendOption, srvOpts []csms.Option, handler func(context.Context, *csms.Conn, bootReq) (bootResp, error)) *backendNode {
	tp.t.Helper()
	be := linkgrpc.NewBackend(linkgrpc.StaticTargets(tp.addr), append([]linkgrpc.BackendOption{
		linkgrpc.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
		linkgrpc.WithResolveInterval(50 * time.Millisecond), linkgrpc.WithDrainTimeout(5 * time.Second)}, beOpts...)...)
	srv := csms.NewServer(append([]csms.Option{csms.WithGatewayLink(be), csms.WithBackendInfo(info)}, srvOpts...)...)
	require.NoError(tp.t, csms.On(srv, bootMsg, handler))
	ctx, cancel := context.WithCancel(context.Background())
	n := &backendNode{srv: srv, stop: cancel, done: make(chan struct{})}
	tp.nodes = append(tp.nodes, n)
	go func() {
		defer close(n.done)
		n.err = srv.ServeGateway(ctx)
	}()
	require.Eventually(tp.t, func() bool { return len(be.Gateways()) == 1 }, 5*time.Second, 10*time.Millisecond)
	// The gateway side must have registered the stream too.
	require.Eventually(tp.t, func() bool { return tp.link.Available(poolOf(info)) }, 5*time.Second, 10*time.Millisecond)
	return n
}

func poolOf(i gateway.BackendInfo) string {
	if i.Pool == "" {
		return gateway.DefaultPool
	}
	return i.Pool
}

func (n *backendNode) shutdown() {
	n.once.Do(func() {
		n.stop()
		<-n.done
		n.srv.Close()
	})
}

func (tp *topology) client(id string) *cp.Client {
	c := cp.NewClient(id, tp.wsURL+id, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(tp.t, c.Connect(ctx))
	return c
}

// close shuts the gateway down first (it still reports the disconnects it
// causes), then the backends, then the gRPC server.
func (tp *topology) close() {
	tp.ts.Close()
	tp.gw.Close()
	for _, n := range tp.nodes {
		n.shutdown()
	}
	tp.gs.Stop()
}

func ok(context.Context, *csms.Conn, bootReq) (bootResp, error) {
	return bootResp{Status: "Accepted", Interval: 60, CurrentTime: "2026-06-15T00:00:00Z"}, nil
}

func TestLink_InboundCallAndCSMSCallOverGRPC(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	tp := newTopology(t)
	defer tp.close()
	n := tp.addBackend(gateway.BackendInfo{Version: "v1"}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		// A handler can call the charger it serves: the call travels back over the
		// same stream.
		got, err := csms.Call(ctx, c, resetMsg, resetReq{Type: "Soft"})
		require.NoError(t, err)
		require.Equal(t, "Accepted", got.Status)
		return ok(ctx, c, r)
	})

	c := tp.client("CP_1")
	defer c.Close()
	require.NoError(t, cp.On(c, resetMsg, func(context.Context, resetReq) (resetResp, error) { return resetResp{Status: "Accepted"}, nil }))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
	require.NoError(t, err)
	require.Equal(t, 60, resp.Interval)

	// Lookup through the session registry, then a call from outside any handler.
	h, err := n.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	got, err := csms.Call(ctx, h, resetMsg, resetReq{Type: "Hard"})
	require.NoError(t, err)
	require.Equal(t, "Accepted", got.Status)
	ref, ok := h.GatewaySession()
	require.True(t, ok)
	require.Equal(t, "gw-1", ref.GatewayID)

	// Control: a disconnect from the backend closes the charger's socket.
	require.NoError(t, h.Close(ctx))
	require.Eventually(t, func() bool { return !c.IsConnected() }, 3*time.Second, 10*time.Millisecond)
}

func TestLink_BackendDrainLosesNothing(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	tp := newTopology(t)
	defer tp.close()
	var a, b atomic.Int64
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	na := tp.addBackend(gateway.BackendInfo{ID: "a"}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		a.Add(1)
		select {
		case entered <- struct{}{}:
			<-release // the first call is held while the backend drains
		default:
		}
		return ok(ctx, c, r)
	})
	c := tp.client("CP_1")
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	first := make(chan error, 1)
	go func() {
		_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
		first <- err
	}()
	<-entered
	tp.addBackend(gateway.BackendInfo{ID: "b"}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		b.Add(1)
		return ok(ctx, c, r)
	})
	// Backend a is asked to drain while its call is still running.
	drained := make(chan struct{})
	go func() { na.stop(); <-na.done; close(drained) }()
	select {
	case <-drained:
		t.Fatal("drain finished with work in flight")
	case <-time.After(300 * time.Millisecond):
	}
	close(release)
	require.NoError(t, <-first, "the in-flight call finishes normally")
	<-drained
	na.srv.Close()

	// Everything after goes to b; no CALLERROR, no disconnect.
	for i := 0; i < 20; i++ {
		_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, a.Load())
	require.EqualValues(t, 20, b.Load())
	require.True(t, c.IsConnected())
}

func TestLink_BrokenStreamAfterDispatchIsNotRetried(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	tp := newTopology(t, linkgrpc.WithUpstreamWait(500*time.Millisecond))
	var runs atomic.Int64
	started := make(chan struct{}, 4)
	block := make(chan struct{})
	n := tp.addBackend(gateway.BackendInfo{}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		runs.Add(1)
		started <- struct{}{}
		<-block
		return ok(ctx, c, r)
	})
	// A second, healthy backend that must NOT receive the retried request.
	var other atomic.Int64
	n2 := tp.addBackend(gateway.BackendInfo{}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		other.Add(1)
		return ok(ctx, c, r)
	})
	c := tp.client("CP_1")
	defer c.Close()
	defer tp.close() // gateway first: the client's disconnect is reported to nobody
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Find out which backend got it by breaking the stream of whichever is blocked.
	errCh := make(chan error, 1)
	go func() {
		_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
		errCh <- err
	}()
	select {
	case <-started:
		// Broke the blocked backend's gRPC connection under the running handler.
		tp.gs.Stop() // kills every stream: the dispatch may have run, so no redispatch
	case err := <-errCh:
		// The call went to the healthy backend; nothing to prove with this run.
		require.NoError(t, err)
		close(block)
		_ = n
		_ = n2
		return
	}
	err := <-errCh
	var ce *ocppj.CallError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)
	require.EqualValues(t, 1, runs.Load())
	require.EqualValues(t, 0, other.Load(), "never redispatched after dispatch")
	require.True(t, c.IsConnected(), "the charger is not disconnected by a backend failure")
	close(block)
}

func TestLink_ProtocolNegotiationAndUnknownFrames(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	tp := newTopology(t)
	defer tp.close()
	cc, err := grpc.NewClient(tp.addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	defer func() { _ = cc.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// A backend whose protocol range is disjoint from the gateway's is rejected
	// loudly (proto 0 and a reason), not degraded.
	st, err := linkpb.NewLinkClient(cc).Attach(ctx)
	require.NoError(t, err)
	require.NoError(t, st.Send(&linkpb.BackendFrame{Body: &linkpb.BackendFrame_Hello{Hello: &linkpb.Hello{
		Backend: &linkpb.BackendInfo{Id: "future", Proto: &linkpb.ProtoRange{Min: 7, Max: 9}},
	}}}))
	f, err := st.Recv()
	require.NoError(t, err)
	require.Zero(t, f.GetHelloAck().GetProto())
	require.Contains(t, f.GetHelloAck().GetReason(), "no common protocol")
	require.False(t, tp.link.Available(gateway.DefaultPool))

	// A compatible stub attaches; frames it does not understand are answered, not
	// fatal: an empty (unknown) body gets Unsupported and the stream stays usable.
	st2, err := linkpb.NewLinkClient(cc).Attach(ctx)
	require.NoError(t, err)
	require.NoError(t, st2.Send(&linkpb.BackendFrame{Body: &linkpb.BackendFrame_Hello{Hello: &linkpb.Hello{
		Backend: &linkpb.BackendInfo{Id: "stub", Proto: &linkpb.ProtoRange{Min: 1, Max: 3}, Caps: []string{"x.future", "send.21"}},
	}}}))
	f, err = st2.Recv()
	require.NoError(t, err)
	require.EqualValues(t, 1, f.GetHelloAck().GetProto(), "min of both maxima")
	require.Equal(t, []string{"send.21"}, f.GetHelloAck().GetCaps(), "intersection of capabilities")

	require.NoError(t, st2.Send(&linkpb.BackendFrame{Id: 41})) // no body: a kind this build does not know
	f, err = st2.Recv()
	require.NoError(t, err)
	require.EqualValues(t, 41, f.Id)
	require.NotNil(t, f.GetUnsupported())

	// An unknown control op is answered ErrUnsupported on the same stream.
	require.NoError(t, st2.Send(&linkpb.BackendFrame{Id: 42, Body: &linkpb.BackendFrame_Control{Control: &linkpb.ControlRequest{
		Target: &linkpb.SessionRef{Cpid: "CP_1", GatewayId: "gw-1"}, Op: 99,
	}}}))
	f, err = st2.Recv()
	require.NoError(t, err)
	require.EqualValues(t, 42, f.Id)
	require.Equal(t, "unsupported", f.GetControlAck().GetError().GetSentinel())

	// And the stream still works: Drain is honoured afterwards.
	require.Eventually(t, func() bool { return tp.link.Available(gateway.DefaultPool) }, time.Second, 10*time.Millisecond)
	require.NoError(t, st2.Send(&linkpb.BackendFrame{Body: &linkpb.BackendFrame_Drain{Drain: &linkpb.Drain{}}}))
	require.Eventually(t, func() bool { return !tp.link.Available(gateway.DefaultPool) }, 2*time.Second, 10*time.Millisecond)
	_ = st2.CloseSend()
}

// A backend without a session registry finds a charger through the link.
func TestLink_LocateWithoutSessionRegistry(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	tp := newTopology(t)
	defer tp.close()
	n := tp.addBackendWith(gateway.BackendInfo{}, nil, nil, ok)
	c := tp.client("CP_1")
	defer c.Close()
	require.NoError(t, cp.On(c, resetMsg, func(context.Context, resetReq) (resetResp, error) { return resetResp{Status: "Accepted"}, nil }))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// The backend has seen no message from CP_1: only Locate can find it.
	h, err := n.srv.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	got, err := csms.Call(ctx, h, resetMsg, resetReq{Type: "Hard"})
	require.NoError(t, err)
	require.Equal(t, "Accepted", got.Status)
	ref, found := h.GatewaySession()
	require.True(t, found)
	require.Equal(t, "gw-1", ref.GatewayID)

	_, err = n.srv.Lookup(ctx, "CP_unknown")
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}

// Protocol skew: ranges overlap on the lower version; the higher end's extra
// capabilities are not used on a stream that negotiated the lower one's.
func TestLink_ProtocolSkew(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	v1 := gateway.ProtoRange{Min: 1, Max: 1}
	v12 := gateway.ProtoRange{Min: 1, Max: 2}
	v2 := gateway.ProtoRange{Min: 2, Max: 2}
	cases := []struct {
		name    string
		gw, be  gateway.ProtoRange
		attach  bool
		wantCap bool
	}{
		{"old backend, new gateway", v12, v1, true, false},
		{"new backend, old gateway", v1, v12, true, false},
		{"both new", v12, v12, true, true},
		{"v2-only backend, old gateway", v1, v2, false, false},
		{"v1-only backend, v2-only gateway", v2, v1, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// A build that speaks v2 also offers the capability that came with it.
			capsOf := func(r gateway.ProtoRange) []string {
				if r.Max >= 2 {
					return []string{"fake.v2"}
				}
				return nil
			}
			tp := newTopology(t, linkgrpc.GatewayProtoForTest(tc.gw, capsOf(tc.gw)...))
			defer tp.close()
			be := linkgrpc.NewBackend(linkgrpc.StaticTargets(tp.addr),
				linkgrpc.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
				linkgrpc.WithResolveInterval(50*time.Millisecond), linkgrpc.WithDrainTimeout(2*time.Second),
				linkgrpc.BackendProtoForTest(tc.be, capsOf(tc.be)...))
			srv := csms.NewServer(csms.WithGatewayLink(be), csms.WithSessionRegistry(tp.reg))
			require.NoError(t, csms.On(srv, bootMsg, ok))
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { defer close(done); _ = srv.ServeGateway(ctx) }()
			defer func() { cancel(); <-done; srv.Close() }()

			if !tc.attach {
				time.Sleep(400 * time.Millisecond) // several attach attempts
				require.False(t, tp.link.Available(gateway.DefaultPool), "disjoint ranges must not attach")
				require.Empty(t, be.Gateways())
				return
			}
			require.Eventually(t, func() bool { return tp.link.Available(gateway.DefaultPool) }, 5*time.Second, 10*time.Millisecond)
			var caps []string
			for _, p := range tp.link.Pools() {
				for _, m := range p.Members {
					caps = m.Caps
				}
			}
			require.Equal(t, tc.wantCap, slices.Contains(caps, "fake.v2"), "caps %v", caps)
			// Traffic flows whatever the negotiated version.
			c := tp.client("CP_1")
			defer c.Close()
			cctx, ccancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer ccancel()
			resp, err := cp.Call(cctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
			require.NoError(t, err)
			require.Equal(t, 60, resp.Interval)
		})
	}
}

// BackendInfo.Actions is filled from the registered handlers, and a router that
// names a pool without a handler for the action falls back to the default pool.
func TestLink_UnhandledActionInRoutedPoolFallsBack(t *testing.T) {
	defer goleak.VerifyNone(t, goleak.IgnoreAnyFunction("google.golang.org/grpc/internal/grpcsync.(*CallbackSerializer).run"))
	router := gateway.RouterFunc(func(gateway.RouteInput) gateway.RouteDecision {
		return gateway.RouteDecision{Pool: "canary", Fallback: gateway.DefaultPool}
	})
	tp := newTopologyWith(t, []csms.Option{csms.WithRouter(router)})
	defer tp.close()
	var stable, canary atomic.Int64
	tp.addBackend(gateway.BackendInfo{ID: "stable"}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		stable.Add(1)
		return ok(ctx, c, r)
	})
	// The canary announces (explicitly) that it handles another action only.
	tp.addBackend(gateway.BackendInfo{ID: "canary", Pool: "canary", Actions: []string{"Other"}}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		canary.Add(1)
		return ok(ctx, c, r)
	})
	var announced []string
	for _, p := range tp.link.Pools() {
		for _, m := range p.Members {
			if m.ID == "stable" {
				announced = m.Actions
			}
		}
	}
	require.Equal(t, []string{"BootNotification"}, announced, "filled from the registered handlers")

	c := tp.client("CP_1")
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for i := 0; i < 5; i++ {
		_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
		require.NoError(t, err)
	}
	require.EqualValues(t, 5, stable.Load())
	require.EqualValues(t, 0, canary.Load())
	require.Equal(t, uint64(5), tp.link.Unhandled()[[2]string{"canary", "BootNotification"}])
}
