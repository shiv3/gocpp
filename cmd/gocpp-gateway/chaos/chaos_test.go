//go:build !short

// Package chaos runs the gateway binary and a test backend as real processes
// (build the binaries, start them, kill -9 them, partition them with a TCP fault
// proxy) and checks the failure semantics of docs/design/gateway.md 2.5: what the
// charger sees, whether it is disconnected, and what callers learn.
package chaos

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/coder/websocket"
	natsd "github.com/nats-io/nats-server/v2/server"
	"github.com/redis/go-redis/v9"
	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var binDir string

// linkKind selects the transport under test: GOCPP_CHAOS_LINK=nats runs the whole
// suite over the NATS link (an embedded nats-server per test), default grpc.
var linkKind = func() string {
	if v := os.Getenv("GOCPP_CHAOS_LINK"); v != "" {
		return v
	}
	return "grpc"
}()

var brokers sync.Map // *testing.T -> *broker

type broker struct {
	mu   sync.Mutex
	ns   *natsd.Server
	addr string
}

func startBroker(t *testing.T, port int) *natsd.Server {
	t.Helper()
	ns, err := natsd.NewServer(&natsd.Options{Host: "127.0.0.1", Port: port, NoLog: true, NoSigs: true})
	require.NoError(t, err)
	go ns.Start()
	require.True(t, ns.ReadyForConnections(10*time.Second))
	return ns
}

// restart stops the broker and starts a new one on the same port.
func (b *broker) restart(t *testing.T, downFor time.Duration) {
	b.mu.Lock()
	defer b.mu.Unlock()
	port := b.ns.Addr().(*net.TCPAddr).Port
	b.ns.Shutdown()
	b.ns.WaitForShutdown()
	time.Sleep(downFor)
	b.ns = startBroker(t, port)
}

// brokerAddr starts (once per test) the embedded NATS server both tiers use in
// nats mode.
func brokerOf(t *testing.T) *broker {
	t.Helper()
	if b, ok := brokers.Load(t); ok {
		return b.(*broker)
	}
	b := &broker{ns: startBroker(t, -1)}
	b.addr = b.ns.Addr().String()
	t.Cleanup(func() { b.mu.Lock(); b.ns.Shutdown(); b.ns.WaitForShutdown(); b.mu.Unlock(); brokers.Delete(t) })
	brokers.Store(t, b)
	return b
}

func brokerAddr(t *testing.T) string { return brokerOf(t).addr }

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gocpp-chaos")
	if err != nil {
		panic(err)
	}
	binDir = dir
	for out, pkg := range map[string]string{"gocpp-gateway": "../", "testbackend": "./testbackend"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(dir, out), pkg)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			fmt.Fprintln(os.Stderr, "build", pkg, err)
			os.Exit(1)
		}
	}
	// Mixed-version runs: use prebuilt binaries instead of the ones just built.
	for out, envName := range map[string]string{"gocpp-gateway": "GOCPP_CHAOS_GATEWAY_BIN", "testbackend": "GOCPP_CHAOS_BACKEND_BIN"} {
		if bin := os.Getenv(envName); bin != "" {
			data, err := os.ReadFile(bin)
			if err != nil {
				panic(err)
			}
			if err := os.WriteFile(filepath.Join(dir, out), data, 0o755); err != nil { //nolint:gosec // test binary
				panic(err)
			}
		}
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// ---- processes -------------------------------------------------------------

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) { s.mu.Lock(); defer s.mu.Unlock(); return s.b.Write(p) }
func (s *syncBuf) String() string              { s.mu.Lock(); defer s.mu.Unlock(); return s.b.String() }

type proc struct {
	t    *testing.T
	name string
	cmd  *exec.Cmd
	out  *syncBuf
	done chan struct{}
}

func start(t *testing.T, name string, args ...string) *proc {
	t.Helper()
	p := &proc{t: t, name: name, out: &syncBuf{}, done: make(chan struct{})}
	p.cmd = exec.Command(filepath.Join(binDir, strings.Split(name, ":")[0]), args...)
	p.cmd.Stdout, p.cmd.Stderr = p.out, p.out
	require.NoError(t, p.cmd.Start())
	go func() { _ = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		_ = p.cmd.Process.Kill()
		<-p.done
		if t.Failed() {
			t.Logf("--- %s output ---\n%s", name, p.out.String())
		}
	})
	return p
}

func (p *proc) kill9() { _ = p.cmd.Process.Signal(syscall.SIGKILL); <-p.done }
func (p *proc) term()  { _ = p.cmd.Process.Signal(syscall.SIGTERM) }
func (p *proc) count(sub string) int {
	return strings.Count(p.out.String(), sub)
}

func freeAddr(t *testing.T) string {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = l.Close() }()
	return l.Addr().String()
}

type gw struct {
	*proc
	id, ws, link, admin string
}

func startGateway(t *testing.T, id, redisAddr string, extra ...string) *gw {
	t.Helper()
	g := &gw{id: id, ws: freeAddr(t), link: freeAddr(t), admin: freeAddr(t)}
	if linkKind == "nats" {
		g.link = brokerAddr(t) // every gateway and backend meets on the broker
		extra = append([]string{"--link", "nats", "--nats-url", "nats://" + g.link, "--nats-accept-timeout", "1500ms"}, extra...)
	}
	args := []string{"serve", "--id", id, "--listen", g.ws, "--link-listen", g.link, "--admin", g.admin,
		"--subprotocols", "ocpp1.6", "--accept-rate", "0", "--upstream-wait", "1s", "--drain-window", "2s",
		"--lease-ttl", "3s", "--lease-heartbeat", "500ms", "--reconcile-interval", "1s", "--claim-timeout", "500ms",
		"--link-keepalive", "1s", "--log-level", "warn"}
	if redisAddr != "" {
		args = append(args, "--redis-addr", redisAddr)
	}
	g.proc = start(t, "gocpp-gateway:"+id, append(args, extra...)...)
	require.Eventually(t, func() bool { return httpOK("http://" + g.admin + "/healthz") }, 10*time.Second, 50*time.Millisecond)
	return g
}

func httpOK(u string) bool {
	resp, err := http.Get(u)
	if err != nil {
		return false
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode == 200
}

func (g *gw) ready() bool { return httpOK("http://" + g.admin + "/readyz") }

func startBackend(t *testing.T, id, redisAddr string, gateways ...string) *proc {
	t.Helper()
	args := []string{"-id", id, "-gateways", strings.Join(gateways, ","), "-redis", redisAddr}
	if linkKind == "nats" {
		args = []string{"-id", id, "-link", "nats", "-gateways", "nats://" + gateways[0], "-redis", redisAddr}
	}
	b := start(t, "testbackend:"+id, args...)
	require.Eventually(t, func() bool { return b.count("READY") > 0 }, 10*time.Second, 20*time.Millisecond)
	return b
}

// ---- TCP fault proxy -------------------------------------------------------

type faultProxy struct {
	l      net.Listener
	target string
	mu     sync.Mutex
	conns  []net.Conn
	block  bool
}

func newProxy(t *testing.T, target string) *faultProxy {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	p := &faultProxy{l: l, target: target}
	t.Cleanup(func() { _ = l.Close(); p.Reset() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			up, err := net.Dial("tcp", target)
			if err != nil {
				_ = c.Close()
				continue
			}
			p.mu.Lock()
			p.conns = append(p.conns, c, up)
			p.mu.Unlock()
			go p.pipe(c, up)
			go p.pipe(up, c)
		}
	}()
	return p
}

func (p *faultProxy) Addr() string { return p.l.Addr().String() }

func (p *faultProxy) pipe(dst, src net.Conn) {
	buf := make([]byte, 32<<10)
	for {
		n, err := src.Read(buf)
		if n > 0 {
			p.mu.Lock()
			blocked := p.block
			p.mu.Unlock()
			if !blocked { // a blackhole: bytes vanish, the connection stays open
				_, _ = dst.Write(buf[:n])
			}
		}
		if err != nil {
			_ = dst.Close()
			return
		}
	}
}

// Blackhole drops all traffic without closing anything (a network partition).
func (p *faultProxy) Blackhole(on bool) { p.mu.Lock(); p.block = on; p.mu.Unlock() }

// Reset closes every proxied connection (a reset).
func (p *faultProxy) Reset() {
	p.mu.Lock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.conns = nil
	p.mu.Unlock()
}

// ---- chargers --------------------------------------------------------------

type bootReq struct {
	ChargePointVendor string `json:"chargePointVendor"`
	ChargePointModel  string `json:"chargePointModel"`
}
type bootResp struct {
	Status      string `json:"status"`
	CurrentTime string `json:"currentTime"`
	Interval    int    `json:"interval"`
}
type hbReq struct{}
type hbResp struct {
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
	hbMsg    = ocppj.Message[hbReq, hbResp]{Action: "Heartbeat", Direction: ocppj.SentByCP}
	resetMsg = ocppj.Message[resetReq, resetResp]{Action: "Reset", Direction: ocppj.SentByCSMS}
)

type charger struct {
	*cp.Client
	disconnects *int32Counter
}

type int32Counter struct {
	mu sync.Mutex
	n  int
}

func (c *int32Counter) inc()     { c.mu.Lock(); c.n++; c.mu.Unlock() }
func (c *int32Counter) get() int { c.mu.Lock(); defer c.mu.Unlock(); return c.n }

func newCharger(t *testing.T, ws, id string) *charger {
	ch := &charger{disconnects: &int32Counter{}}
	ch.Client = cp.NewClient(id, "ws://"+ws+"/ocpp/"+id, cp.WithSubProtocols("ocpp1.6"),
		cp.WithOnDisconnect(func(error) { ch.disconnects.inc() }), cp.WithCallTimeout(10*time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, ch.Connect(ctx))
	t.Cleanup(ch.Close)
	return ch
}

func (c *charger) boot(ctx context.Context, vendor string) error {
	_, err := cp.Call(ctx, c.Client, bootMsg, bootReq{ChargePointVendor: vendor, ChargePointModel: "m"})
	return err
}

func (c *charger) heartbeat(ctx context.Context) error {
	_, err := cp.Call(ctx, c.Client, hbMsg, hbReq{})
	return err
}

// caller is an in-process processing-tier server in the caller role: it issues
// CSMS calls through the same gateways, looking sessions up in the registry.
func newCaller(t *testing.T, redisAddr string, gateways ...string) *csms.Server {
	var be gateway.BackendLink
	var liveGateways func() []string
	if linkKind == "nats" {
		nc, err := linknats.Connect("nats://"+gateways[0], "caller")
		require.NoError(t, err)
		nb, err := linknats.NewBackend(nc, linknats.WithHelloInterval(500*time.Millisecond))
		require.NoError(t, err)
		t.Cleanup(func() { nb.Close(); nc.Close() })
		be, liveGateways = nb, nb.Gateways
	} else {
		gb := linkgrpc.NewBackend(linkgrpc.StaticTargets(gateways...),
			linkgrpc.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())), linkgrpc.WithResolveInterval(100*time.Millisecond))
		be, liveGateways = gb, gb.Gateways
	}
	rdb := redis.NewClient(&redis.Options{Addr: redisAddr})
	srv := csms.NewServer(csms.WithGatewayLink(be), csms.WithSessionRegistry(registryredis.New(rdb)),
		csms.WithBackendInfo(gateway.BackendInfo{ID: "caller", Role: gateway.RoleCaller}))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { defer close(done); _ = srv.ServeGateway(ctx) }()
	t.Cleanup(func() { cancel(); <-done; srv.Close(); _ = rdb.Close() })
	require.Eventually(t, func() bool { return len(liveGateways()) == len(gateways) }, 10*time.Second, 50*time.Millisecond)
	return srv
}

func ctxT(d time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), d)
}

func callErrCode(err error) ocppj.ErrorCode {
	var ce *ocppj.CallError
	if ok := asCallError(err, &ce); ok {
		return ce.Code
	}
	return ""
}

func asCallError(err error, target **ocppj.CallError) bool {
	for err != nil {
		if ce, ok := err.(*ocppj.CallError); ok {
			*target = ce
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// ---- tests -----------------------------------------------------------------

// SIGTERM a backend under load: it drains (Nack + redispatch), so the charger
// sees no CALLERROR and no disconnect.
func TestChaos_BackendSIGTERMUnderLoadIsInvisible(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	b1 := startBackend(t, "b1", mr.Addr(), g.link)
	b2 := startBackend(t, "b2", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c := newCharger(t, g.ws, "CP_1")

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var calls, failures int
	var firstErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			ctx, cancel := ctxT(5 * time.Second)
			err := c.heartbeat(ctx)
			cancel()
			calls++
			if err != nil {
				failures++
				if firstErr == nil {
					firstErr = err
				}
			}
		}
	}()
	require.Eventually(t, func() bool { return b1.count("HANDLED Heartbeat") > 20 && b2.count("HANDLED Heartbeat") > 20 }, 10*time.Second, 50*time.Millisecond)
	b1.term()
	select {
	case <-b1.done:
	case <-time.After(15 * time.Second):
		t.Fatal("backend did not exit after SIGTERM")
	}
	before := b2.count("HANDLED Heartbeat")
	time.Sleep(500 * time.Millisecond)
	close(stop)
	wg.Wait()
	require.Zero(t, failures, "first error: %v", firstErr)
	require.Greater(t, calls, 50)
	require.Greater(t, b2.count("HANDLED Heartbeat"), before, "the surviving backend took over")
	require.Zero(t, c.disconnects.get())
	require.Contains(t, b1.out.String(), "EXIT")
}

// kill -9 a backend while its handler runs: the charger gets InternalError once,
// the request is not redispatched, and the charger stays connected.
func TestChaos_BackendKillMidHandlerNoRedispatchNoDisconnect(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	b1 := startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c := newCharger(t, g.ws, "CP_1")

	errCh := make(chan error, 1)
	go func() {
		ctx, cancel := ctxT(20 * time.Second)
		defer cancel()
		errCh <- c.boot(ctx, "hold")
	}()
	require.Eventually(t, func() bool { return b1.count("HANDLED BootNotification b1 CP_1 hold") == 1 }, 10*time.Second, 20*time.Millisecond)
	// A second backend appears; it must never see the held request.
	b2 := startBackend(t, "b2", mr.Addr(), g.link)
	b1.kill9()
	err := <-errCh
	require.Equal(t, ocppj.ErrorCodeInternalError, callErrCode(err), "got %v", err)
	require.Zero(t, b2.count("HANDLED BootNotification"), "not redispatched after dispatch")
	require.True(t, c.IsConnected())
	require.Zero(t, c.disconnects.get())

	// Following messages are served normally by the other backend.
	ctx, cancel := ctxT(10 * time.Second)
	defer cancel()
	require.NoError(t, c.heartbeat(ctx))
	require.Equal(t, 1, b2.count("HANDLED Heartbeat"))
}

// kill -9 a gateway: a caller whose command was already written learns
// SentNoResponse, the dead gateway's sessions stop being routable once its lease
// lapses (a caller then gets NotSent, not a hang), and the charger comes back
// through the other gateway with a higher epoch.
func TestChaos_GatewayKillSessionsReestablishedAndSentReported(t *testing.T) {
	mr := miniredis.RunT(t)
	g1 := startGateway(t, "gw-1", mr.Addr())
	g2 := startGateway(t, "gw-2", mr.Addr())
	startBackend(t, "b1", mr.Addr(), g1.link, g2.link)
	caller := newCaller(t, mr.Addr(), g1.link, g2.link)
	require.Eventually(t, func() bool { return g1.ready() && g2.ready() }, 10*time.Second, 50*time.Millisecond)

	connect := func(g *gw, release chan struct{}, entered chan struct{}) *cp.Client {
		c := cp.NewClient("CP_1", "ws://"+g.ws+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"), cp.WithCallTimeout(20*time.Second))
		require.NoError(t, cp.On(c, resetMsg, func(ctx context.Context, _ resetReq) (resetResp, error) {
			select {
			case entered <- struct{}{}:
			default:
			}
			select {
			case <-release:
			case <-ctx.Done():
			}
			return resetResp{Status: "Accepted"}, nil
		}))
		ctx, cancel := ctxT(5 * time.Second)
		defer cancel()
		require.NoError(t, c.Connect(ctx))
		t.Cleanup(c.Close)
		return c
	}
	release1, entered1 := make(chan struct{}), make(chan struct{}, 1)
	connect(g1, release1, entered1)

	ctx, cancel := ctxT(30 * time.Second)
	defer cancel()
	var h *csms.Conn
	require.Eventually(t, func() bool {
		var err error
		h, err = caller.Lookup(ctx, "CP_1")
		return err == nil
	}, 5*time.Second, 50*time.Millisecond)
	ref1, _ := h.GatewaySession()
	require.Equal(t, "gw-1", ref1.GatewayID)

	callErr := make(chan error, 1)
	go func() {
		_, err := csms.Call(ctx, h, resetMsg, resetReq{Type: "Soft"})
		callErr <- err
	}()
	<-entered1 // the frame is on the charger
	g1.kill9()
	err := <-callErr
	require.Error(t, err)
	require.Equal(t, gateway.SentNoResponse, gateway.SentOf(err), "written, no answer: %v", err)
	close(release1)

	// Until the lease lapses the registry still names the dead gateway; after it
	// does, the session is not routable and a call fails fast with NotSent.
	mr.FastForward(5 * time.Second)
	_, err = csms.Call(ctx, h, resetMsg, resetReq{Type: "Soft"})
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
	require.Equal(t, gateway.NotSent, gateway.SentOf(err))

	// The charger reconnects through the other gateway: next epoch, calls work.
	release2, entered2 := make(chan struct{}), make(chan struct{}, 1)
	close(release2)
	connect(g2, release2, entered2)
	require.Eventually(t, func() bool {
		h2, err := caller.Lookup(ctx, "CP_1")
		if err != nil {
			return false
		}
		ref2, _ := h2.GatewaySession()
		if ref2.GatewayID != "gw-2" {
			return false
		}
		_, err = csms.Call(ctx, h2, resetMsg, resetReq{Type: "Hard"})
		return err == nil && ref2.Epoch > ref1.Epoch
	}, 10*time.Second, 100*time.Millisecond)
}

// Partition gateway and backend (a blackhole, not a reset): the link keepalive
// notices within seconds, the held CALL gets InternalError after the hold, the
// charger stays connected, and everything works again once the partition heals.
func TestChaos_GatewayBackendPartition(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	px := newProxy(t, g.link)
	b := startBackend(t, "b1", mr.Addr(), px.Addr())
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c := newCharger(t, g.ws, "CP_1")
	ctx, cancel := ctxT(30 * time.Second)
	defer cancel()
	require.NoError(t, c.heartbeat(ctx))

	px.Blackhole(true)
	start := time.Now()
	var err error
	for err == nil && time.Since(start) < 15*time.Second {
		err = c.heartbeat(ctx) // the first ones may still be answered until the keepalive fires
	}
	require.Equal(t, ocppj.ErrorCodeInternalError, callErrCode(err), "got %v", err)
	require.True(t, c.IsConnected())
	require.Zero(t, c.disconnects.get())
	require.Eventually(t, func() bool { return !g.ready() }, 5*time.Second, 50*time.Millisecond, "not ready without a backend")

	px.Blackhole(false)
	px.Reset() // the backend's dead connection is gone for good; it redials
	require.Eventually(t, func() bool { return c.heartbeat(ctx) == nil }, 15*time.Second, 200*time.Millisecond)
	require.True(t, g.ready())
	_ = b
}

// Gateway and registry partitioned: existing sessions keep working, a new
// charger is accepted unclaimed after the claim timeout, and the registry
// converges (the session gets claimed) after the partition heals.
func TestChaos_GatewayRegistryPartition(t *testing.T) {
	mr := miniredis.RunT(t)
	px := newProxy(t, mr.Addr())
	g := startGateway(t, "gw-1", px.Addr())
	startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c1 := newCharger(t, g.ws, "CP_1")
	ctx, cancel := ctxT(30 * time.Second)
	defer cancel()
	require.NoError(t, c1.heartbeat(ctx))

	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = rdb.Close() }()
	reg := registryredis.New(rdb)
	require.Eventually(t, func() bool { _, ok, _ := reg.Lookup(ctx, "CP_1"); return ok }, 5*time.Second, 50*time.Millisecond)

	px.Blackhole(true)
	// Existing session: inbound needs no registry.
	for i := 0; i < 5; i++ {
		require.NoError(t, c1.heartbeat(ctx))
	}
	// New charger: accepted unclaimed after the claim timeout, and served.
	c2 := newCharger(t, g.ws, "CP_2")
	require.NoError(t, c2.heartbeat(ctx))
	_, claimed, _ := reg.Lookup(ctx, "CP_2")
	require.False(t, claimed, "not claimed while the registry is unreachable")

	px.Blackhole(false)
	px.Reset()
	require.Eventually(t, func() bool { _, ok, _ := reg.Lookup(ctx, "CP_2"); return ok }, 20*time.Second, 200*time.Millisecond,
		"reconcile claims the session once the registry is back")
	require.NoError(t, c2.heartbeat(ctx))
	require.True(t, c1.IsConnected() && c2.IsConnected())
}

// The same charger id on two gateways: the registry orders them by epoch and the
// older socket is closed by the reconcile pass; the newest one is routed to.
func TestChaos_DuplicateIDAcrossGateways(t *testing.T) {
	mr := miniredis.RunT(t)
	g1 := startGateway(t, "gw-1", mr.Addr())
	g2 := startGateway(t, "gw-2", mr.Addr())
	startBackend(t, "b1", mr.Addr(), g1.link, g2.link)
	caller := newCaller(t, mr.Addr(), g1.link, g2.link)
	require.Eventually(t, func() bool { return g1.ready() && g2.ready() }, 10*time.Second, 50*time.Millisecond)
	c1 := newCharger(t, g1.ws, "CP_1")
	ctx, cancel := ctxT(30 * time.Second)
	defer cancel()
	require.NoError(t, c1.heartbeat(ctx))
	c2 := newCharger(t, g2.ws, "CP_1")
	require.NoError(t, c2.heartbeat(ctx))

	require.Eventually(t, func() bool { return !c1.IsConnected() }, 15*time.Second, 100*time.Millisecond, "the superseded socket is closed")
	require.True(t, c2.IsConnected())
	h, err := caller.Lookup(ctx, "CP_1")
	require.NoError(t, err)
	ref, _ := h.GatewaySession()
	require.Equal(t, "gw-2", ref.GatewayID)
}

// The pre-flight probe fails while no backend is attached (the gateway must not
// receive chargers yet) and passes once one is; /readyz follows the soft cap.
func TestChaos_ProbeAndReadiness(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr(), "--soft-max-sessions", "1")
	runProbe := func() error {
		cmd := exec.Command(filepath.Join(binDir, "gocpp-gateway"), "probe", "--url", "ws://"+g.ws+"/ocpp/", "--id", "gw-1", "--timeout", "5s")
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Logf("probe: %s", out)
		}
		return err
	}
	require.Error(t, runProbe(), "no backend: the gateway cannot serve chargers")
	require.False(t, g.ready())

	startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	require.NoError(t, runProbe())

	// Over the soft cap: not ready (the load balancer stops sending more), but
	// established sessions are untouched and upgrades still work.
	c := newCharger(t, g.ws, "CP_1")
	require.Eventually(t, func() bool { return !g.ready() }, 5*time.Second, 50*time.Millisecond)
	ctx, cancel := ctxT(10 * time.Second)
	defer cancel()
	require.NoError(t, c.heartbeat(ctx))
	c2 := newCharger(t, g.ws, "CP_2")
	require.NoError(t, c2.heartbeat(ctx))
}

// NATS only: the broker restarts. Requests in flight fail once (the link cannot
// know whether they were taken), the charger stays connected, both tiers
// reconnect by themselves and traffic resumes without any process restart.
func TestChaos_NATSBrokerRestart(t *testing.T) {
	if linkKind != "nats" {
		t.Skip("NATS link only")
	}
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr())
	startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c := newCharger(t, g.ws, "CP_1")
	ctx, cancel := ctxT(30 * time.Second)
	defer cancel()
	require.NoError(t, c.heartbeat(ctx))

	// A handler is running when the broker goes away.
	errCh := make(chan error, 1)
	go func() { errCh <- c.boot(ctx, "slow") }()
	time.Sleep(200 * time.Millisecond)
	brokerOf(t).restart(t, 500*time.Millisecond)
	err := <-errCh
	if err != nil { // the answer may also have made it out before the broker stopped
		require.Equal(t, ocppj.ErrorCodeInternalError, callErrCode(err), "got %v", err)
	}
	require.True(t, c.IsConnected())
	require.Zero(t, c.disconnects.get())
	require.Eventually(t, func() bool { return c.heartbeat(ctx) == nil }, 20*time.Second, 200*time.Millisecond)
	require.True(t, g.ready())
}

// --edge-heartbeat: the gateway answers Heartbeat itself (the backend never sees
// it) while other messages still reach the backend.
func TestChaos_EdgeHeartbeat(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr(), "--edge-heartbeat")
	b := startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c := newCharger(t, g.ws, "CP_1")
	ctx, cancel := ctxT(10 * time.Second)
	defer cancel()
	for i := 0; i < 20; i++ {
		require.NoError(t, c.heartbeat(ctx))
	}
	require.NoError(t, c.boot(ctx, "x"))
	require.Zero(t, b.count("HANDLED Heartbeat"))
	require.Equal(t, 1, b.count("HANDLED BootNotification"))
}

// Per-tenant session cap at the gateway: the tenant is a request header, the
// refusal is a 503 before the upgrade, and another tenant is unaffected.
func TestChaos_TenantSessionCap(t *testing.T) {
	mr := miniredis.RunT(t)
	g := startGateway(t, "gw-1", mr.Addr(), "--tenant-header", "X-Tenant", "--tenant-max-sessions", "1")
	startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	dial := func(id, tenant string) (*websocket.Conn, int, error) {
		ctx, cancel := ctxT(5 * time.Second)
		defer cancel()
		c, resp, err := websocket.Dial(ctx, "ws://"+g.ws+"/ocpp/"+id, &websocket.DialOptions{
			Subprotocols: []string{"ocpp1.6"}, HTTPHeader: http.Header{"X-Tenant": []string{tenant}}})
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		return c, status, err
	}
	c1, _, err := dial("CP_1", "a")
	require.NoError(t, err)
	defer func() { _ = c1.CloseNow() }()
	_, status, err := dial("CP_2", "a")
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, status)
	c3, _, err := dial("CP_3", "b")
	require.NoError(t, err)
	_ = c3.CloseNow()
}

// --tap-jsonl: the gateway exports frame and session events to a file, and a
// SIGTERM flushes what is buffered. Heartbeats can be left out.
func TestChaos_TapJSONL(t *testing.T) {
	mr := miniredis.RunT(t)
	file := filepath.Join(t.TempDir(), "tap.jsonl")
	g := startGateway(t, "gw-1", mr.Addr(), "--tap-jsonl", file, "--tap-skip-heartbeat")
	b := startBackend(t, "b1", mr.Addr(), g.link)
	require.Eventually(t, g.ready, 5*time.Second, 50*time.Millisecond)
	c := newCharger(t, g.ws, "CP_1")
	ctx, cancel := ctxT(10 * time.Second)
	defer cancel()
	require.NoError(t, c.heartbeat(ctx))
	require.NoError(t, c.boot(ctx, "x"))
	c.Close()
	time.Sleep(200 * time.Millisecond) // the close event is produced after the socket ends
	b.term()                           // with the gRPC link a gateway waits for its backends' streams before it exits
	<-b.done
	g.proc.term()
	select {
	case <-g.proc.done:
	case <-time.After(15 * time.Second):
		t.Fatal("gateway did not stop")
	}
	data, err := os.ReadFile(file)
	require.NoError(t, err)
	var boots, hbs, opened, closed int
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var ev struct {
			Kind    string `json:"kind"`
			Source  string `json:"source"`
			Action  string `json:"action"`
			Session string `json:"session"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &ev), line)
		require.Equal(t, "gw-1", ev.Source)
		switch {
		case ev.Kind == "frame" && ev.Action == "BootNotification":
			boots++
		case ev.Kind == "frame" && ev.Action == "Heartbeat":
			hbs++
		case ev.Session == "opened":
			opened++
		case ev.Session == "closed":
			closed++
		}
	}
	require.Equal(t, 1, boots)
	require.Zero(t, hbs)
	require.Equal(t, 1, opened)
	require.Equal(t, 1, closed)
}
