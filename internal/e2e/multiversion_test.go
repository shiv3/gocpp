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

func TestE2E_SubprotocolNegotiation(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.0.1", "ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	base := "ws" + ts.URL[len("http"):]

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c16 := cp.NewClient("CP_16", base+"/ocpp/CP_16", cp.WithSubProtocols("ocpp1.6"))
	require.NoError(t, c16.Connect(ctx))
	defer c16.Close()
	require.Equal(t, "ocpp1.6", c16.NegotiatedProtocol())

	c201 := cp.NewClient("CP_201", base+"/ocpp/CP_201", cp.WithSubProtocols("ocpp2.1", "ocpp2.0.1"))
	require.NoError(t, c201.Connect(ctx))
	defer c201.Close()
	require.Equal(t, "ocpp2.0.1", c201.NegotiatedProtocol())
}
