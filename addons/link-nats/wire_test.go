package linknats_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/v16"
	"github.com/stretchr/testify/require"
)

// frames runs a deterministic exchange through a raw WebSocket against the
// handler newServer returns and gives every frame the server wrote, in order.
// (The same exchange as csms's split-mode wire test, which runs it over the
// in-process link.)
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
	script := []string{
		`[2,"a1","BootNotification",{"chargePointVendor":"V","chargePointModel":"M"}]`,
		`[2,"a2","BootNotification",{"chargePointVendor":"V"}]`,                           // schema violation
		`[2,"a3","BootNotification",{"chargePointVendor":"boom","chargePointModel":"M"}]`, // handler panic
		`[2,"a4","BootNotification",{"chargePointVendor":"nope","chargePointModel":"M"}]`, // handler CallError
		`[2,"a5","Unknown",{}]`, // not implemented
		`[2,"a6","BootNotification",{"chargePointVendor":"V","chargePointModel":"M","x":1}]`, // additional property
		`[2,"a7","BootNotification",{"chargePointVendor":5,"chargePointModel":"M"}]`,         // type
		`[2,"a8","BootNotification",{"chargePointVendor":"V","chargePointModel":"M"}]`,
	}
	for _, f := range script {
		require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(f)))
		_, _, err := c.Read(ctx)
		require.NoError(t, err)
	}
	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(out) == len(script) }, 2*time.Second, 5*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return append([]string(nil), out...)
}

// The same deterministic exchange gives byte-identical frames from an embedded
// server and from a gateway with a backend behind it over NATS: validation
// errors, handler panics and CALLERRORs all come out of the backend's pipeline
// exactly as in embedded mode.
func TestLink_WireIdenticalToEmbedded(t *testing.T) {
	leakCheck(t)
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	handler := func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		switch req.ChargePointVendor {
		case "boom":
			panic("boom")
		case "nope":
			return bootResp{}, ocppj.NewCallError(ocppj.ErrorCodeNotSupported, "no thanks", map[string]any{"k": "v"})
		}
		return bootResp{Status: "Accepted", Interval: 300, CurrentTime: "2026-06-15T00:00:00Z"}, nil
	}
	common := []csms.Option{csms.WithSchemaRegistry(reg), csms.WithStrictSchema(true)}

	embedded := frames(t, func(obs csms.Option) (http.Handler, func()) {
		s := csms.NewServer(append(common[:len(common):len(common)], obs)...)
		require.NoError(t, csms.On(s, bootMsg, handler))
		return s.Handler(), s.Close
	})
	split := frames(t, func(obs csms.Option) (http.Handler, func()) {
		ns := startNATS(t)
		link, err := linknats.NewGateway(connect(t, ns.ClientURL()))
		require.NoError(t, err)
		gw := csms.NewGatewayServer(link, append(common[:len(common):len(common)], obs)...)
		nc := connect(t, ns.ClientURL())
		bl, err := linknats.NewBackend(nc, linknats.WithHelloInterval(helloEvery))
		require.NoError(t, err)
		be := csms.NewServer(append(common[:len(common):len(common)], csms.WithGatewayLink(bl))...)
		require.NoError(t, csms.On(be, bootMsg, handler))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() { defer close(done); _ = be.ServeGateway(ctx) }()
		require.Eventually(t, func() bool { return link.Available(gateway.DefaultPool) }, 5*time.Second, 10*time.Millisecond)
		return gw.Handler(), func() {
			gw.Close()
			cancel()
			<-done
			be.Close()
			bl.Close()
			link.Close()
		}
	})
	require.Len(t, embedded, 8)
	require.Equal(t, embedded, split)
	for _, f := range split {
		var arr []json.RawMessage
		require.NoError(t, json.Unmarshal([]byte(f), &arr))
	}
}
