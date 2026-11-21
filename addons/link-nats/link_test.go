package linknats_test

import (
	"context"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	natsd "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/shiv3/gocpp/addons/link-grpc/linkpb"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
	"google.golang.org/protobuf/proto"
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

const helloEvery = 50 * time.Millisecond // leases lapse after 150 ms

func startNATS(t *testing.T) *natsd.Server {
	t.Helper()
	ns, err := natsd.NewServer(&natsd.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	go ns.Start()
	require.True(t, ns.ReadyForConnections(10*time.Second))
	t.Cleanup(func() { ns.Shutdown(); ns.WaitForShutdown() })
	return ns
}

func connect(t *testing.T, url string) *nats.Conn {
	t.Helper()
	nc, err := nats.Connect(url, nats.ReconnectBufSize(-1), nats.NoReconnect())
	require.NoError(t, err)
	t.Cleanup(nc.Close)
	return nc
}

// topology is one gateway (csms.Server + NATS link) and any number of backends,
// all in this process but talking through a real nats-server over loopback.
type topology struct {
	t     *testing.T
	ns    *natsd.Server
	reg   storage.SessionRegistry
	link  *linknats.Gateway
	gw    *csms.Server
	ts    *httptest.Server
	wsURL string
	nodes []*backendNode
}

func newTopology(t *testing.T, gwOpts ...linknats.GatewayOption) *topology {
	t.Helper()
	tp := &topology{t: t, reg: memory.NewSessionRegistry(), ns: startNATS(t)}
	var err error
	tp.link, err = linknats.NewGateway(connect(t, tp.ns.ClientURL()), gwOpts...)
	require.NoError(t, err)
	tp.gw = csms.NewGatewayServer(tp.link, csms.WithGatewayID("gw-1"), csms.WithSessionRegistry(tp.reg))
	tp.ts = httptest.NewServer(tp.gw.Handler())
	tp.wsURL = "ws" + tp.ts.URL[4:] + "/ocpp/"
	return tp
}

type backendNode struct {
	be   *linknats.Backend
	nc   *nats.Conn
	srv  *csms.Server
	stop context.CancelFunc
	done chan struct{}
	once sync.Once
}

func (tp *topology) addBackend(info gateway.BackendInfo, handler func(context.Context, *csms.Conn, bootReq) (bootResp, error)) *backendNode {
	tp.t.Helper()
	nc := connect(tp.t, tp.ns.ClientURL())
	be, err := linknats.NewBackend(nc, linknats.WithHelloInterval(helloEvery), linknats.WithDrainTimeout(5*time.Second))
	require.NoError(tp.t, err)
	srv := csms.NewServer(csms.WithGatewayLink(be), csms.WithSessionRegistry(tp.reg), csms.WithBackendInfo(info))
	require.NoError(tp.t, csms.On(srv, bootMsg, handler))
	ctx, cancel := context.WithCancel(context.Background())
	n := &backendNode{be: be, nc: nc, srv: srv, stop: cancel, done: make(chan struct{})}
	tp.nodes = append(tp.nodes, n)
	go func() {
		defer close(n.done)
		_ = srv.ServeGateway(ctx)
	}()
	require.Eventually(tp.t, func() bool { return len(be.Gateways()) == 1 }, 5*time.Second, 10*time.Millisecond)
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
		n.be.Close()
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
// causes), then the backends.
func (tp *topology) close() {
	tp.ts.Close()
	tp.gw.Close()
	for _, n := range tp.nodes {
		n.shutdown()
	}
	tp.link.Close()
}

func ok(context.Context, *csms.Conn, bootReq) (bootResp, error) {
	return bootResp{Status: "Accepted", Interval: 60, CurrentTime: "2026-06-15T00:00:00Z"}, nil
}

func TestLink_InboundCallAndCSMSCallOverNATS(t *testing.T) {
	leakCheck(t)
	tp := newTopology(t)
	defer tp.close()
	n := tp.addBackend(gateway.BackendInfo{Version: "v1"}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		// A handler can call the charger it serves: the call travels back through
		// the gateway's subject.
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
	leakCheck(t)
	tp := newTopology(t)
	defer tp.close()
	var a, b atomic.Int64
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	na := tp.addBackend(gateway.BackendInfo{}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
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
	tp.addBackend(gateway.BackendInfo{}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
		b.Add(1)
		return ok(ctx, c, r)
	})
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

	for i := 0; i < 20; i++ {
		_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
		require.NoError(t, err)
	}
	require.EqualValues(t, 1, a.Load())
	require.EqualValues(t, 20, b.Load())
	require.True(t, c.IsConnected())
}

func TestLink_DeadBackendAfterDispatchIsNotRetried(t *testing.T) {
	leakCheck(t)
	tp := newTopology(t, linknats.WithUpstreamWait(500*time.Millisecond))
	var runs, other atomic.Int64
	started := make(chan struct{}, 4)
	block := make(chan struct{})
	defer close(block)
	// Two backends; whichever receives the CALL blocks, then its connection to
	// NATS is cut (kill -9 as the broker sees it).
	mk := func(counter *atomic.Int64) *backendNode {
		return tp.addBackend(gateway.BackendInfo{}, func(ctx context.Context, c *csms.Conn, r bootReq) (bootResp, error) {
			counter.Add(1)
			started <- struct{}{}
			<-block
			return ok(ctx, c, r)
		})
	}
	n1, n2 := mk(&runs), mk(&other)
	c := tp.client("CP_1")
	defer c.Close()
	defer tp.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
		errCh <- err
	}()
	<-started
	n1.nc.Close()
	n2.nc.Close() // both: whichever holds the request is gone, nobody else exists
	err := <-errCh
	var ce *ocppj.CallError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)
	require.EqualValues(t, 1, runs.Load()+other.Load(), "never redispatched after dispatch")
	require.True(t, c.IsConnected(), "the charger is not disconnected by a backend failure")
}

// rawBackend speaks the wire by hand: Hello with a protocol range, then frames
// the gateway side must answer without breaking anything.
func TestLink_ProtocolNegotiationAndUnknownFrames(t *testing.T) {
	leakCheck(t)
	tp := newTopology(t)
	defer tp.close()
	nc := connect(t, tp.ns.ClientURL())
	hello := func(b *linkpb.BackendInfo) *linkpb.HelloAck {
		data, _ := proto.Marshal(&linkpb.Hello{Backend: b})
		m, err := nc.Request("gocpp.hello", data, 2*time.Second)
		require.NoError(t, err)
		var ack linkpb.HelloAck
		require.NoError(t, proto.Unmarshal(m.Data, &ack))
		return &ack
	}

	// A disjoint protocol range is rejected loudly (proto 0 and a reason).
	ack := hello(&linkpb.BackendInfo{Id: "future", Proto: &linkpb.ProtoRange{Min: 7, Max: 9}})
	require.Zero(t, ack.Proto)
	require.Contains(t, ack.Reason, "no common protocol")
	require.False(t, tp.link.Available(gateway.DefaultPool))

	// A compatible one: min of the maxima, intersection of capabilities.
	ack = hello(&linkpb.BackendInfo{Id: "stub", Proto: &linkpb.ProtoRange{Min: 1, Max: 3}, Caps: []string{"x.future", "send.21"},
		Labels: map[string]string{"link.hello_ms": "60000"}})
	require.EqualValues(t, 1, ack.Proto)
	require.Equal(t, []string{"send.21"}, ack.Caps)
	require.Equal(t, "gw-1", ack.Gateway.GetId())
	require.True(t, tp.link.Available(gateway.DefaultPool))

	// An unknown downstream op is answered Unsupported on the same subject space.
	m, err := nc.Request("gocpp.gw.gw-1.teleport", nil, 2*time.Second)
	require.NoError(t, err)
	require.NotEmpty(t, m.Header.Get("Gocpp-Unsupported"))

	// An unknown control op is answered ErrUnsupported.
	data, _ := proto.Marshal(&linkpb.ControlRequest{Target: &linkpb.SessionRef{Cpid: "CP_1", GatewayId: "gw-1"}, Op: 99})
	m, err = nc.Request("gocpp.gw.gw-1.control", data, 2*time.Second)
	require.NoError(t, err)
	var a linkpb.Ack
	require.NoError(t, proto.Unmarshal(m.Data, &a))
	require.Equal(t, "unsupported", a.GetError().GetSentinel())

	// Drain is honoured and acknowledged.
	data, _ = proto.Marshal(&linkpb.Hello{Backend: &linkpb.BackendInfo{Id: "stub"}})
	_, err = nc.Request("gocpp.drain", data, 2*time.Second)
	require.NoError(t, err)
	require.False(t, tp.link.Available(gateway.DefaultPool))
}

func TestLink_NoBackendHoldsThenInternalError(t *testing.T) {
	leakCheck(t)
	tp := newTopology(t, linknats.WithUpstreamWait(300*time.Millisecond))
	defer tp.close()
	n := tp.addBackend(gateway.BackendInfo{}, ok)
	c := tp.client("CP_1")
	defer c.Close()
	n.shutdown() // the pool is empty now
	require.Eventually(t, func() bool { return !tp.link.Available(gateway.DefaultPool) }, 2*time.Second, 10*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	start := time.Now()
	_, err := cp.Call(ctx, c, bootMsg, bootReq{ChargePointVendor: "x"})
	var ce *ocppj.CallError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, ocppj.ErrorCodeInternalError, ce.Code)
	require.GreaterOrEqual(t, time.Since(start), 250*time.Millisecond, "held for a backend first")
	require.True(t, c.IsConnected())
}

// leakCheck runs last (cleanups are LIFO), after the broker and every
// connection have been shut down.
func leakCheck(t *testing.T) {
	t.Cleanup(func() {
		goleak.VerifyNone(t, goleak.IgnoreAnyFunction("github.com/nats-io/nats-server/v2/server.(*Server).handleSignals"))
	})
}
