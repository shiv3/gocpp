package cp_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestClient_ConnectsToServer(t *testing.T) {
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
	}, 2*time.Second, 20*time.Millisecond)
}

func TestOnAny_CatchAllServesUnregisteredActions(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := cp.NewClient("CP_ANY", "ws"+ts.URL[len("http"):]+"/ocpp/CP_ANY", cp.WithSubProtocols("ocpp1.6"))
	var seen string
	cp.OnAny(client, func(ctx context.Context, action string, payload []byte) ([]byte, error) {
		seen = action
		return []byte(`{"status":"Accepted"}`), nil
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()
	require.Eventually(t, func() bool { _, ok := srv.Get("CP_ANY"); return ok }, 2*time.Second, 20*time.Millisecond)

	conn, ok := srv.Get("CP_ANY")
	require.True(t, ok)
	out, err := csms.CallRaw(ctx, conn, "ClearCache", []byte(`{}`))
	require.NoError(t, err)
	require.JSONEq(t, `{"status":"Accepted"}`, string(out))
	require.Equal(t, "ClearCache", seen)

	cp.OnAny(client, nil)
	_, err = csms.CallRaw(ctx, conn, "ClearCache", []byte(`{}`))
	require.Error(t, err)
}
