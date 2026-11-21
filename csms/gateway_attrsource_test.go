package csms_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type fakeSource struct {
	mu    sync.Mutex
	attrs map[string]string
	err   error
	delay time.Duration
}

func (f *fakeSource) set(m map[string]string, err error) {
	f.mu.Lock()
	f.attrs, f.err = m, err
	f.mu.Unlock()
}

func (f *fakeSource) Attrs(ctx context.Context, _ string) (map[string]string, error) {
	f.mu.Lock()
	m, err, d := f.attrs, f.err, f.delay
	f.mu.Unlock()
	if d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return m, err
}

// External attributes are added with the ext. prefix at connect, never override
// what the backend set, follow the source on refresh, and a failing or slow
// source means no external attributes, counted.
func TestGateway_AttrSource(t *testing.T) {
	defer goleak.VerifyNone(t)
	hub := inproc.New()
	src := &fakeSource{attrs: map[string]string{"site": "berlin", "gone": "x"}}
	var ok, bad atomic.Int64
	var seen sync.Map
	router := gateway.RouterFunc(func(in gateway.RouteInput) gateway.RouteDecision {
		if in.Action == "BootNotification" {
			seen.Store("attrs", in.Attrs)
		}
		return gateway.RouteDecision{Pool: gateway.DefaultPool}
	})
	gw := csms.NewGatewayServer(hub.Gateway(), csms.WithRouter(router),
		csms.WithAttrSource(src, csms.AttrSourceOptions{Timeout: 100 * time.Millisecond, Refresh: 100 * time.Millisecond,
			OnResult: func(r string) {
				if r == "ok" {
					ok.Add(1)
				} else {
					bad.Add(1)
				}
			}}))
	be := csms.NewServer(csms.WithGatewayLink(hub.Backend()))
	require.NoError(t, csms.On(be, gwBootMsg, func(context.Context, *csms.Conn, gwBoot) (gwBootResp, error) {
		return gwBootResp{Status: "Accepted"}, nil
	}))
	drain := hub.AttachBackend(be.Upstream(), gateway.BackendInfo{})
	ts := httptest.NewServer(gw.Handler())
	defer func() { ts.Close(); gw.Close(); _ = drain(context.Background()); be.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	boot := func(id string) map[string]string {
		c := cp.NewClient(id, "ws"+ts.URL[4:]+"/ocpp/"+id, cp.WithSubProtocols("ocpp1.6"))
		require.NoError(t, c.Connect(ctx))
		t.Cleanup(c.Close)
		_, err := cp.Call(ctx, c, gwBootMsg, gwBoot{ChargePointVendor: "x"})
		require.NoError(t, err)
		v, _ := seen.Load("attrs")
		m, _ := v.(map[string]string)
		return m
	}
	m := boot("CP_1")
	require.Equal(t, "berlin", m["ext.site"])
	require.Equal(t, "x", m["ext.gone"])

	// Refresh: the source changes, the session follows (and a removed key goes away).
	src.set(map[string]string{"site": "paris"}, nil)
	require.Eventually(t, func() bool {
		c, ok := gw.Get("CP_1")
		return ok && c.Attrs()["ext.site"] == "paris" && c.Attrs()["ext.gone"] == ""
	}, 3*time.Second, 20*time.Millisecond)

	// A failing source keeps the old attributes on refresh and gives none at connect.
	src.set(nil, errors.New("down"))
	time.Sleep(300 * time.Millisecond)
	c1, _ := gw.Get("CP_1")
	require.Equal(t, "paris", c1.Attrs()["ext.site"])
	require.Empty(t, boot("CP_2")["ext.site"], "source down: session starts without external attributes")
	src.set(map[string]string{"a": "b"}, nil)
	src.mu.Lock()
	src.delay = time.Second
	src.mu.Unlock()
	require.Empty(t, boot("CP_3")["ext.a"], "slower than the timeout")
	require.NotZero(t, bad.Load())
	require.NotZero(t, ok.Load())
}
