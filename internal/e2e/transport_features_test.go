package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v16client "github.com/shiv3/gocpp/v16/client"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// Exercises the real WebSocket handshake path (which FakeWS unit tests bypass):
// the client sends HTTP Basic credentials and the CSMS authenticates them.
func TestE2E_ClientBasicAuth(t *testing.T) {
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithAuthenticator(auth.BasicAuth(func(cpID, password string) (auth.Identity, error) {
			if cpID != "CP_1" || password != "s3cret" {
				return auth.Identity{}, auth.ErrUnauthorized
			}
			return auth.Identity{CPID: cpID}, nil
		})),
	)
	require.NoError(t, csms.On(srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
		return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Correct credentials: handshake succeeds and a call round-trips.
	ok := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"), cp.WithBasicAuth("CP_1", "s3cret"))
	require.NoError(t, ok.Connect(ctx))
	defer ok.Close()
	_, err := v16client.NewCP(ok).Heartbeat(ctx, v16msg.HeartbeatRequest{})
	require.NoError(t, err)

	// Wrong credentials: the server rejects the upgrade (HTTP 401).
	bad := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"), cp.WithBasicAuth("CP_1", "wrong"))
	require.Error(t, bad.Connect(ctx))
}

// Exercises the real TLS handshake via WithTLSConfig over wss://.
func TestE2E_ClientTLS(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, csms.On(srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
		return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
	}))

	ts := httptest.NewTLSServer(srv.Handler())
	defer ts.Close()

	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	url := "wss" + ts.URL[len("https"):] + "/ocpp/CP_1"

	client := cp.NewClient("CP_1", url,
		cp.WithSubProtocols("ocpp1.6"),
		cp.WithTLSConfig(&tls.Config{RootCAs: pool}),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	_, err := v16client.NewCP(client).Heartbeat(ctx, v16msg.HeartbeatRequest{})
	require.NoError(t, err)
}

// Exercises WithHeartbeatInterval: the CP auto-sends OCPP Heartbeat over a real
// connection without the application calling Call.
func TestE2E_ClientHeartbeatAutoSend(t *testing.T) {
	var beats atomic.Int32
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, csms.On(srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
		beats.Add(1)
		return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"

	client := cp.NewClient("CP_1", url,
		cp.WithSubProtocols("ocpp1.6"),
		cp.WithHeartbeatInterval(50*time.Millisecond),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	require.Eventually(t, func() bool { return beats.Load() >= 2 }, 3*time.Second, 10*time.Millisecond)
}

// Exercises the active-ping keepalive: with no application traffic, the WebSocket
// ping/pong round trips must keep resetting the read watchdog on both ends, so an
// idle connection survives well past ReadTimeout. This integration (coder/websocket
// auto-pong -> OnPongReceived -> NoteActivity) cannot be covered by FakeWS tests.
func TestE2E_WebSocketPingKeepsIdleConnectionAlive(t *testing.T) {
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithWebSocketPingInterval(50*time.Millisecond),
		csms.WithWebSocketPongWait(time.Second),
		csms.WithWebSocketReadTimeout(300*time.Millisecond),
	)
	require.NoError(t, csms.On(srv, v16p.Heartbeat, func(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
		return v16msg.HeartbeatResponse{CurrentTime: time.Now()}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"

	client := cp.NewClient("CP_1", url,
		cp.WithSubProtocols("ocpp1.6"),
		cp.WithWebSocketPingInterval(50*time.Millisecond),
		cp.WithWebSocketPongWait(time.Second),
		cp.WithWebSocketReadTimeout(300*time.Millisecond),
	)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	// Idle for ~3x the read timeout. Only ping/pong traffic flows.
	time.Sleep(1 * time.Second)

	require.True(t, client.IsConnected())
	_, err := v16client.NewCP(client).Heartbeat(ctx, v16msg.HeartbeatRequest{})
	require.NoError(t, err)
}

// Exercises the passive read watchdog (ocpp-go-style: the server does not actively
// ping and expects the peer to). A raw client that connects but never reads sends
// nothing and never auto-pongs, so the server must reap it via ReadTimeout.
func TestE2E_WebSocketReadTimeoutReapsSilentPeer(t *testing.T) {
	disconnected := make(chan error, 1)
	srv := csms.NewServer(
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithWebSocketPingInterval(0), // passive: rely on the peer to ping
		csms.WithWebSocketReadTimeout(200*time.Millisecond),
		csms.WithOnDisconnect(func(_ *csms.Conn, err error) {
			select {
			case disconnected <- err:
			default:
			}
		}),
	)

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_SILENT"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	rawConn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6"},
	})
	require.NoError(t, err)
	defer func() { _ = rawConn.CloseNow() }()

	select {
	case err := <-disconnected:
		require.ErrorContains(t, err, "read idle timeout")
	case <-time.After(3 * time.Second):
		t.Fatal("server did not reap the silent peer within the read timeout")
	}
}
