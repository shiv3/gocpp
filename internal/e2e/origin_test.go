package e2e

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

// TestE2E_OriginCheck verifies WebSocket origin verification: a cross-origin
// upgrade is rejected by default and accepted when origin checking is disabled
// via WithInsecureSkipVerifyOrigin.
func TestE2E_OriginCheck(t *testing.T) {
	const crossOrigin = "http://evil.example.com"

	t.Run("rejected by default", func(t *testing.T) {
		srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()

		url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
		client := cp.NewClient("CP_1", url,
			cp.WithSubProtocols("ocpp1.6"),
			cp.WithHTTPHeader("Origin", crossOrigin),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.Error(t, client.Connect(ctx))
		client.Close()
	})

	t.Run("allowed with WithInsecureSkipVerifyOrigin", func(t *testing.T) {
		srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithInsecureSkipVerifyOrigin())
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()

		url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_2"
		client := cp.NewClient("CP_2", url,
			cp.WithSubProtocols("ocpp1.6"),
			cp.WithHTTPHeader("Origin", crossOrigin),
		)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		require.NoError(t, client.Connect(ctx))
		client.Close()
	})

	t.Run("custom WithCheckOrigin decides", func(t *testing.T) {
		// Allow only a specific origin via a custom predicate.
		srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithCheckOrigin(func(r *http.Request) bool {
			return r.Header.Get("Origin") == "http://trusted.example.com"
		}))
		ts := httptest.NewServer(srv.Handler())
		defer ts.Close()
		base := "ws" + ts.URL[len("http"):]

		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()

		rejected := cp.NewClient("CP_3", base+"/ocpp/CP_3",
			cp.WithSubProtocols("ocpp1.6"), cp.WithHTTPHeader("Origin", crossOrigin))
		require.Error(t, rejected.Connect(ctx))
		rejected.Close()

		allowed := cp.NewClient("CP_4", base+"/ocpp/CP_4",
			cp.WithSubProtocols("ocpp1.6"), cp.WithHTTPHeader("Origin", "http://trusted.example.com"))
		require.NoError(t, allowed.Connect(ctx))
		allowed.Close()
	})
}

// TestE2E_Shutdown verifies graceful shutdown drains live connections.
func TestE2E_Shutdown(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	require.Eventually(t, func() bool {
		_, ok := srv.Get("CP_1")
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutCancel()
	require.NoError(t, srv.Shutdown(shutCtx))

	_, ok := srv.Get("CP_1")
	require.False(t, ok)
}
