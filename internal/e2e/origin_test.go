package e2e

import (
	"context"
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
}
