package transport_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/transport"
)

func TestCoderWS_RoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"ocpp1.6"}})
		if err != nil {
			t.Errorf("websocket.Accept() error = %v", err)
			return
		}
		ws := transport.NewCoderWS(c)
		// echo one message
		msg, err := ws.Read(r.Context())
		if err != nil {
			return
		}
		_ = ws.Write(r.Context(), msg)
		_ = ws.Close(transport.StatusCode(websocket.StatusNormalClosure), "done")
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := "ws" + srv.URL[len("http"):]
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	if err != nil {
		t.Fatalf("websocket.Dial() error = %v", err)
	}
	client := transport.NewCoderWS(c)

	if err := client.Write(ctx, []byte("ping")); err != nil {
		t.Fatalf("client.Write() error = %v", err)
	}
	got, err := client.Read(ctx)
	if err != nil {
		t.Fatalf("client.Read() error = %v", err)
	}
	if string(got) != "ping" {
		t.Fatalf("client.Read() = %q, want %q", got, "ping")
	}
	_ = client.Close(transport.StatusCode(websocket.StatusNormalClosure), "bye")
}
