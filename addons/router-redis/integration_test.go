//go:build integration

package routerredis

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestRedisRouterIntegration(t *testing.T) {
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	rdb := redis.NewClient(&redis.Options{Addr: addr})
	defer rdb.Close()
	if err := rdb.Ping(ctx).Err(); err != nil {
		t.Skipf("Redis is not available at %s: %v", addr, err)
	}

	prefix := "gocpp:router:integration:" + time.Now().Format("20060102150405.000000000")
	reg := newFakeRegistry(map[string]string{"CP_1": "B"})
	instA := New(rdb, "A", reg, WithChannelPrefix(prefix), WithRequestTimeout(250*time.Millisecond))
	instB := New(rdb, "B", reg, WithChannelPrefix(prefix), WithRequestTimeout(250*time.Millisecond))

	serveCtx, stopServe := context.WithCancel(context.Background())
	defer stopServe()
	errCh := make(chan error, 1)
	go func() {
		errCh <- instB.ServeRemote(serveCtx, func(context.Context, string, string, []byte) ([]byte, error) {
			return []byte(`{"status":"Accepted"}`), nil
		})
	}()

	var (
		resp []byte
		err  error
	)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		callCtx, callCancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		resp, err = instA.CallRemote(callCtx, "CP_1", "Reset", []byte(`{}`))
		callCancel()
		if err == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("CallRemote never succeeded: %v", err)
	}
	if string(resp) != `{"status":"Accepted"}` {
		t.Fatalf("response = %s, want accepted", resp)
	}

	stopServe()
	if err := <-errCh; !errors.Is(err, context.Canceled) {
		t.Fatalf("ServeRemote error = %v, want context.Canceled", err)
	}
}
