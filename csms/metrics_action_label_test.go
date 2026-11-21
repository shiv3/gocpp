package csms

import (
	"context"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/shiv3/gocpp/core/observability/metrics/prom"
	"github.com/stretchr/testify/require"
)

// A charger that invents action names must not create metric series.
func TestMetricsActionLabelIsBounded(t *testing.T) {
	reg := prometheus.NewRegistry()
	srv := NewServer(WithMetrics(prom.New(reg)), WithSubProtocols("ocpp1.6"))
	defer srv.Close()
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[4:]+"/ocpp/CP_1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()
	const flood = 300
	for i := 0; i < flood; i++ {
		frame := `[2,"m` + strconv.Itoa(i) + `","Invented` + strconv.Itoa(i) + `",{}]`
		require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(frame)))
		_, _, err := c.Read(ctx) // the NotImplemented CALLERROR
		require.NoError(t, err)
	}
	// A known action (no handler registered, so also NotImplemented) keeps its name.
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"known","Heartbeat",{}]`)))
	_, _, err = c.Read(ctx)
	require.NoError(t, err)

	mfs, err := reg.Gather()
	require.NoError(t, err)
	actions := map[string]bool{}
	for _, mf := range mfs {
		if mf.GetName() != "gocpp_calls_total" && mf.GetName() != "gocpp_call_duration_seconds" {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == "action" {
					actions[l.GetValue()] = true
				}
			}
		}
	}
	require.LessOrEqual(t, len(actions), 3, "action label values: %v", actions)
	require.NotContains(t, actions, "Invented0")
}
