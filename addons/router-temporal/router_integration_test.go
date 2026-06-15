//go:build integration

package routertemporal

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/storage/memory"
	"go.temporal.io/sdk/client"
)

func TestIntegrationCallRemoteRequiresTemporalServer(t *testing.T) {
	targetHostPort := os.Getenv("TEMPORAL_HOST_PORT")
	if targetHostPort == "" {
		t.Skip("set TEMPORAL_HOST_PORT to run integration tests")
	}

	c, err := client.Dial(client.Options{HostPort: targetHostPort})
	if err != nil {
		t.Fatalf("dial temporal: %v", err)
	}
	defer c.Close()

	reg := memory.NewConnectionRegistry()
	if err := reg.PutGlobal(context.Background(), "CP_1", "instance-b"); err != nil {
		t.Fatalf("put global binding: %v", err)
	}

	routerA := New(c, "instance-a", reg)
	routerB := New(c, "instance-b", reg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		errCh <- routerB.ServeRemote(ctx, func(context.Context, string, string, []byte) ([]byte, error) {
			return []byte(`{"status":"Accepted"}`), nil
		})
	}()

	callCtx, callCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer callCancel()
	resp, err := routerA.CallRemote(callCtx, "CP_1", "Reset", []byte(`{}`))
	if err != nil {
		t.Fatalf("call remote: %v", err)
	}
	if string(resp) != `{"status":"Accepted"}` {
		t.Fatalf("response = %s", resp)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != context.Canceled {
			t.Fatalf("serve remote error = %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("worker did not stop")
	}
}
