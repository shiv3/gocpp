package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v21client "github.com/shiv3/gocpp/v21/client"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	v21p "github.com/shiv3/gocpp/v21/profiles"
	"github.com/stretchr/testify/require"
)

// extResponseRecorder records the Sec-WebSocket-Extensions header written on the
// 101 Switching Protocols upgrade response.
type extResponseRecorder struct {
	http.ResponseWriter
	ext *atomic.Value // string
}

func (w extResponseRecorder) WriteHeader(code int) {
	if code == http.StatusSwitchingProtocols {
		w.ext.Store(w.Header().Get("Sec-WebSocket-Extensions"))
	}
	w.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying hijackable writer,
// which coder/websocket needs to take over the connection.
func (w extResponseRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func TestE2E_CompressionNegotiated(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1")) // compression default ON
	require.NoError(t, csms.On(srv, v21p.Heartbeat, func(_ context.Context, _ *csms.Conn, _ v21msg.HeartbeatRequest) (v21msg.HeartbeatResponse, error) {
		return v21msg.HeartbeatResponse{CurrentTime: time.Now().UTC()}, nil
	}))

	var ext atomic.Value
	ext.Store("")
	h := srv.Handler()
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(extResponseRecorder{ResponseWriter: w, ext: &ext}, r)
	})
	ts := httptest.NewServer(wrapped)
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1")) // compression default ON

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	_, err := v21client.NewCP(client).Heartbeat(ctx, v21msg.HeartbeatRequest{})
	require.NoError(t, err)

	require.True(t, strings.Contains(ext.Load().(string), "permessage-deflate"),
		"expected permessage-deflate negotiated, got %q", ext.Load())
}

func TestE2E_CompressionDisabledStillConnects(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.1"), csms.WithCompression(transport.CompressionDisabled))
	require.NoError(t, csms.On(srv, v21p.Heartbeat, func(_ context.Context, _ *csms.Conn, _ v21msg.HeartbeatRequest) (v21msg.HeartbeatResponse, error) {
		return v21msg.HeartbeatResponse{CurrentTime: time.Now().UTC()}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp2.1"), cp.WithCompression(transport.CompressionDisabled))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()
	_, err := v21client.NewCP(client).Heartbeat(ctx, v21msg.HeartbeatRequest{})
	require.NoError(t, err)
}
