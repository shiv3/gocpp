//go:build interop

package interop

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	v21client "github.com/shiv3/gocpp/v21/client"
	v21msg "github.com/shiv3/gocpp/v21/messages"
	"github.com/stretchr/testify/require"
)

// TestInterop_CitrineOS21_Boot drives a gocpp charge point against a real
// CitrineOS 2.1 CSMS. It is the seed for the full Phase 6 interop matrix.
//
// It is build-tagged `interop` and additionally skips unless CITRINEOS_WS_URL
// points at a running CitrineOS instance (e.g. ws://localhost:8080/ocpp), so the
// suite stays runnable in CI without pinning a Docker image yet.
func TestInterop_CitrineOS21_Boot(t *testing.T) {
	wsURL := os.Getenv("CITRINEOS_WS_URL")
	if wsURL == "" {
		t.Skip("set CITRINEOS_WS_URL=ws://host:port/ocpp to run the CitrineOS 2.1 interop smoke test")
	}

	client := cp.NewClient("CP_TEST", wsURL+"/CP_TEST", cp.WithSubProtocols("ocpp2.1"))
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	resp, err := v21client.NewCP(client).BootNotification(ctx, v21msg.BootNotificationRequest{
		ChargingStation: v21msg.ChargingStationType{VendorName: "gocpp", Model: "Interop"},
		Reason:          "PowerUp",
	})
	require.NoError(t, err)
	require.NotEmpty(t, resp.Status)
}
