package cp_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestHandshakeError_Unauthorized(t *testing.T) {
	a := auth.BasicAuth(func(cpID, pw string) (auth.Identity, error) { return auth.Identity{}, auth.ErrUnauthorized })
	ts := httptest.NewServer(csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithAuthenticator(a)).Handler())
	defer ts.Close()

	client := cp.NewClient("CP_1", "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"), cp.WithBasicAuth("CP_1", "x"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Connect(ctx)
	require.Error(t, err)
	he, ok := cp.AsHandshakeError(err)
	require.True(t, ok, "got %T %v", err, err)
	require.Equal(t, http.StatusUnauthorized, he.StatusCode)
	require.False(t, he.Temporary())
	require.Contains(t, err.Error(), "401", "message unchanged: it is the dial error's text")
}

func TestHandshakeError_FullServerIsTemporary(t *testing.T) {
	ts := httptest.NewServer(csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithMaxConnections(1)).Handler())
	defer ts.Close()
	base := "ws" + ts.URL[len("http"):] + "/ocpp/"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	first := cp.NewClient("A", base+"A", cp.WithSubProtocols("ocpp1.6"))
	require.NoError(t, first.Connect(ctx))
	defer first.Close()
	err := cp.NewClient("B", base+"B", cp.WithSubProtocols("ocpp1.6")).Connect(ctx)
	he, ok := cp.AsHandshakeError(err)
	require.True(t, ok, "got %v", err)
	require.Equal(t, http.StatusServiceUnavailable, he.StatusCode)
	require.True(t, he.Temporary())
	require.NotEmpty(t, he.Header.Get("Retry-After"))
}

func TestHandshakeError_NetworkErrorIsNotHandshakeError(t *testing.T) {
	ts := httptest.NewServer(http.NotFoundHandler())
	url := "ws" + ts.URL[len("http"):]
	ts.Close()
	err := cp.NewClient("A", url, cp.WithSubProtocols("ocpp1.6")).Connect(context.Background())
	require.Error(t, err)
	_, ok := cp.AsHandshakeError(err)
	require.False(t, ok)
}

func TestOnDisconnect_CloseErrorFromServerClose(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	got := make(chan error, 1)
	client := cp.NewClient("CP_1", "ws"+ts.URL[len("http"):]+"/ocpp/CP_1", cp.WithSubProtocols("ocpp1.6"),
		cp.WithOnDisconnect(func(err error) {
			select {
			case got <- err:
			default:
			}
		}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()
	require.Eventually(t, func() bool { _, ok := srv.Get("CP_1"); return ok }, 2*time.Second, 10*time.Millisecond)
	c, _ := srv.Get("CP_1")
	require.NoError(t, c.Close(ctx))

	select {
	case err := <-got:
		ce, ok := transport.AsCloseError(err)
		require.True(t, ok, "got %v", err)
		require.True(t, ce.Normal(), "server close is a normal closure, got %d", ce.Code)
	case <-time.After(5 * time.Second):
		t.Fatal("no disconnect")
	}
}
