package sim_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-sim/sim"
	"github.com/shiv3/gocpp/csms"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

func TestRunner_DrivesSession(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	require.NoError(t, csms.On(srv, v16p.BootNotification, func(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	sc := sim.Scenario{
		Version: "1.6", CPID: "CP_SIM",
		CSMSURL: "ws" + ts.URL[len("http"):] + "/ocpp/",
		Steps: []sim.Step{
			{Action: "BootNotification", Payload: map[string]any{"chargePointVendor": "Acme", "chargePointModel": "M1"}},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	results, err := sim.Run(ctx, sc)
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.NoError(t, results[0].Err)
	require.Contains(t, string(results[0].Response), "Accepted")
}
