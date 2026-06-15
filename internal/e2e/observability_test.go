package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestE2E_EmitsHandlerSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))

	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithTracerProvider(tp))
	require.NoError(t, csms.On(srv, bootMsg, func(ctx context.Context, c *csms.Conn, req bootReq) (bootResp, error) {
		return bootResp{Status: "Accepted"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	_, err := cp.Call(ctx, client, bootMsg, bootReq{ChargePointVendor: "Acme", ChargePointModel: "M1"})
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		for _, s := range rec.Ended() {
			if s.Name() == "ocpp.handler" {
				return true
			}
		}
		return false
	}, 2*time.Second, 20*time.Millisecond)
}
