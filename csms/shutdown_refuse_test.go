package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func requireRefusedWith503(ctx context.Context, t *testing.T, url string) {
	t.Helper()
	_, resp, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.Error(t, err)
	require.NotNil(t, resp)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

func TestServer_ShutdownRefusesNewUpgrades(t *testing.T) {
	connects := make(chan struct{}, 4)
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithOnConnect(func(*csms.Conn) { connects <- struct{}{} }),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + ts.URL[len("http"):] + "/ocpp/"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	open, _, err := websocket.Dial(ctx, base+"cp1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	<-connects

	require.NoError(t, srv.Shutdown(ctx))

	// The connection open before Shutdown got a normal close.
	_, _, err = open.Read(ctx)
	var ce websocket.CloseError
	require.ErrorAs(t, err, &ce)
	require.Equal(t, websocket.StatusNormalClosure, ce.Code)

	// A new charge point is refused with 503 before any upgrade.
	requireRefusedWith503(ctx, t, base+"cp2")
	select {
	case <-connects:
		t.Fatal("a connection was accepted after Shutdown")
	default:
	}
}

func TestServer_RefusesUpgradesDuringShutdownAndAfterClose(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + ts.URL[len("http"):] + "/ocpp/"
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A peer that never answers the close handshake keeps Shutdown in progress.
	hold, _, err := websocket.Dial(ctx, base+"cp1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = hold.CloseNow() }()
	require.Eventually(t, func() bool { _, ok := srv.Get("cp1"); return ok }, 2*time.Second, 10*time.Millisecond)

	sctx, scancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer scancel()
	done := make(chan error, 1)
	go func() { done <- srv.Shutdown(sctx) }()

	require.Eventually(t, func() bool {
		_, resp, err := websocket.Dial(ctx, base+"cp2", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		return err != nil && resp != nil && resp.StatusCode == http.StatusServiceUnavailable
	}, 2*time.Second, 10*time.Millisecond)
	_ = <-done

	srv2 := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts2 := httptest.NewServer(srv2.Handler())
	defer ts2.Close()
	srv2.Close()
	requireRefusedWith503(ctx, t, "ws"+ts2.URL[len("http"):]+"/ocpp/cp3")
}
