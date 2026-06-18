package e2e

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v16client "github.com/shiv3/gocpp/v16/client"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// TestE2E_DataTransfer_Bidirectional verifies that DataTransfer works in both
// directions using the single generated v16p.DataTransfer descriptor, as OCPP
// 1.6 allows DataTransfer to be initiated by either peer.
func TestE2E_DataTransfer_Bidirectional(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))

	// CP-initiated DataTransfer: CSMS handles it.
	require.NoError(t, csms.On(srv, v16p.DataTransfer, func(_ context.Context, _ *csms.Conn, req v16msg.DataTransferRequest) (v16msg.DataTransferResponse, error) {
		require.Equal(t, "cp-vendor", req.VendorID)
		return v16msg.DataTransferResponse{Status: v16msg.DataTransferResponseStatusAccepted}, nil
	}))

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"
	client := cp.NewClient("CP_1", url, cp.WithSubProtocols("ocpp1.6"))

	// CSMS-initiated DataTransfer: the charge point handles it.
	require.NoError(t, cp.On(client, v16p.DataTransfer, func(_ context.Context, req v16msg.DataTransferRequest) (v16msg.DataTransferResponse, error) {
		require.Equal(t, "csms-vendor", req.VendorID)
		return v16msg.DataTransferResponse{Status: v16msg.DataTransferResponseStatusAccepted}, nil
	}))

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	// Direction 1: CP -> CSMS
	resp, err := v16client.NewCP(client).DataTransfer(ctx, v16msg.DataTransferRequest{VendorID: "cp-vendor"})
	require.NoError(t, err)
	require.Equal(t, v16msg.DataTransferResponseStatusAccepted, resp.Status)

	// Direction 2: CSMS -> CP
	var conn *csms.Conn
	require.Eventually(t, func() bool {
		c, ok := srv.Get("CP_1")
		if ok {
			conn = c
		}
		return ok
	}, 5*time.Second, 10*time.Millisecond)

	resp2, err := v16client.NewCSMS(conn).DataTransfer(ctx, v16msg.DataTransferRequest{VendorID: "csms-vendor"})
	require.NoError(t, err)
	require.Equal(t, v16msg.DataTransferResponseStatusAccepted, resp2.Status)
}
