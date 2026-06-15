package csms_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func basicAuthHeader(user, pass string) http.Header {
	h := http.Header{}
	h.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+pass)))
	return h
}

func TestServer_RejectsUnauthenticated(t *testing.T) {
	a := auth.BasicAuth(func(cpID, pw string) (auth.Identity, error) {
		if pw == "secret" {
			return auth.Identity{CPID: cpID}, nil
		}
		return auth.Identity{}, auth.ErrUnauthorized
	})
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithAuthenticator(a))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_1"

	// No credentials -> handshake rejected.
	_, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.Error(t, err)

	// Correct credentials -> accepted and registered.
	good, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		Subprotocols: []string{"ocpp1.6"},
		HTTPHeader:   basicAuthHeader("CP_1", "secret"),
	})
	require.NoError(t, err)
	defer good.Close(websocket.StatusNormalClosure, "")
	require.Eventually(t, func() bool {
		_, ok := srv.Get("CP_1")
		return ok
	}, 2*time.Second, 20*time.Millisecond)
}
