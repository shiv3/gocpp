package e2e

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
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
	_, err := cp.Call(ctx, ok, v16p.Heartbeat, v16msg.HeartbeatRequest{})
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

	_, err := cp.Call(ctx, client, v16p.Heartbeat, v16msg.HeartbeatRequest{})
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
