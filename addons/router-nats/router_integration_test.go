//go:build integration

package routernats

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
)

func TestLiveNATSRequestReply(t *testing.T) {
	url := os.Getenv("NATS_URL")
	if url == "" {
		url = nats.DefaultURL
	}

	ncA, err := nats.Connect(url, nats.Name("gocpp-router-nats-test-a"), nats.Timeout(time.Second), nats.NoReconnect())
	if err != nil {
		t.Skipf("NATS server not available at %s: %v", url, err)
	}
	defer ncA.Close()

	ncB, err := nats.Connect(url, nats.Name("gocpp-router-nats-test-b"), nats.Timeout(time.Second), nats.NoReconnect())
	if err != nil {
		t.Skipf("NATS server not available at %s: %v", url, err)
	}
	defer ncB.Close()

	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	routerA := New(ncA, "A", reg)
	routerB := New(ncB, "B", reg)

	serveCtx, cancelServe := context.WithCancel(context.Background())
	defer cancelServe()
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- routerB.ServeRemote(serveCtx, func(_ context.Context, cpID, action string, req []byte) ([]byte, error) {
			if cpID != "CP_1" || action != "Reset" {
				t.Errorf("handler args = %q, %q", cpID, action)
			}
			if string(req) != `{}` {
				t.Errorf("handler payload = %s", req)
			}
			return []byte(`{"status":"Accepted"}`), nil
		})
	}()

	deadline := time.Now().Add(3 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		select {
		case err := <-serveErr:
			t.Fatalf("ServeRemote returned before call: %v", err)
		default:
		}

		callCtx, cancelCall := context.WithTimeout(context.Background(), 500*time.Millisecond)
		resp, err := routerA.CallRemote(callCtx, "CP_1", "Reset", []byte(`{}`))
		cancelCall()
		if err == nil {
			if string(resp) != `{"status":"Accepted"}` {
				t.Fatalf("response = %s", resp)
			}
			cancelServe()
			if err := <-serveErr; !errors.Is(err, context.Canceled) {
				t.Fatalf("ServeRemote error = %v, want %v", err, context.Canceled)
			}
			return
		}
		lastErr = err
		time.Sleep(50 * time.Millisecond)
	}

	t.Fatalf("CallRemote did not succeed before deadline; last error: %v", lastErr)
}
