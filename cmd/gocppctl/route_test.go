package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway/routepolicy"
	"github.com/stretchr/testify/require"
)

func ctl(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(context.Background(), args, &out, &errb)
	return out.String(), errb.String(), code
}

func write(t *testing.T, name, body string) string {
	p := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(p, []byte(body), 0o600))
	return p
}

// fleet simulates gateways: an engine watching the registry and reporting its
// revision in its lease, as cmd/gocpp-gateway does.
func fleet(t *testing.T, addr string, watching ...bool) {
	t.Helper()
	for i, w := range watching {
		id := "gw-" + strconv.Itoa(i)
		rdb := redis.NewClient(&redis.Options{Addr: addr})
		reg := registryredis.New(rdb)
		var mu sync.Mutex
		labels := map[string]string{"version": "v1", "caps": "route.v1", "policy_rev": "0"}
		beat := func() {
			mu.Lock()
			defer mu.Unlock()
			cp := map[string]string{}
			for k, v := range labels {
				cp[k] = v
			}
			_ = reg.Heartbeat(context.Background(), storage.GatewayInfo{ID: id, Boot: "b", State: storage.GatewayServing, Labels: cp, Sessions: 10}, time.Hour)
		}
		beat()
		e := routepolicy.NewEngine(routepolicy.WithOnApply(func(rev uint64) {
			mu.Lock()
			labels["policy_rev"] = strconv.FormatUint(rev, 10)
			mu.Unlock()
			beat()
		}))
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan struct{})
		go func() {
			defer close(done)
			if w {
				_ = e.Watch(ctx, routepolicy.StoreSource{Store: reg})
			}
		}()
		t.Cleanup(func() { cancel(); <-done; _ = rdb.Close() })
	}
}

const step = `
revision: 1
salt: r1
rules:
  - name: canary
    pool: canary
    match: { percent: %d }
`

func TestRouteWorkflow(t *testing.T) {
	mr := miniredis.RunT(t)
	fleet(t, mr.Addr(), true, true, true)
	conn := []string{"--redis-addr", mr.Addr()}
	with := func(a ...string) []string { return append(a, conn...) }

	// offline validation
	good := write(t, "p.yaml", "revision: 1\nrules:\n  - {name: c, pool: canary, match: {percent: 5}}\n")
	out, _, code := ctl(t, "route", "validate", good)
	require.Equal(t, 0, code, out)
	require.Contains(t, out, "ok:")
	bad := write(t, "bad.yaml", "rules:\n  - {name: c, pool: canary, actions: [MeterValues]}\n")
	_, errs, code := ctl(t, "route", "validate", bad)
	require.Equal(t, exitErr, code)
	require.Contains(t, errs, "transaction group")

	// show with nothing stored
	out, _, code = ctl(t, with("route", "show")...)
	require.Equal(t, 0, code)
	require.Contains(t, out, "no policy stored")

	// apply 5%, wait for convergence
	f1 := write(t, "s1.yaml", "revision: 0\nsalt: r1\nrules:\n  - {name: canary, pool: canary, match: {percent: 5}}\n")
	out, errs, code = ctl(t, with("route", "apply", f1, "--wait", "5s")...)
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "revision 1 is active on 3/3 gateways")

	f2 := write(t, "s2.yaml", "salt: r1\nrules:\n  - {name: canary, pool: canary, match: {percent: 50}}\n")
	out, errs, code = ctl(t, with("route", "apply", f2, "--wait", "5s", "--expect-rev", "1")...)
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "revision 2 is active on 3/3")
	// compare-and-set conflict
	_, errs, code = ctl(t, with("route", "apply", f2, "--expect-rev", "1")...)
	require.Equal(t, exitErr, code)
	require.Contains(t, errs, "changed concurrently")

	out, _, code = ctl(t, with("route", "status")...)
	require.Equal(t, 0, code)
	require.Contains(t, out, "stored revision 2, active on 3/3")
	out, _, _ = ctl(t, with("route", "show", "--history")...)
	require.Contains(t, out, "2 ")
	out, _, _ = ctl(t, with("route", "show", "-o", "json")...)
	require.Contains(t, out, `"revision": 2`)

	// rollback is a new revision with the old contents
	out, errs, code = ctl(t, with("route", "rollback", "--wait", "5s")...)
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "revision 3 written (contents of revision 1, holds ignored)")
	require.Contains(t, out, "active on 3/3")
}

func TestApplyDoesNotReportSuccessWhenAGatewayLags(t *testing.T) {
	mr := miniredis.RunT(t)
	fleet(t, mr.Addr(), true, false) // gw-1 never sees the store
	f := write(t, "s.yaml", "rules:\n  - {name: canary, pool: canary, match: {percent: 5}}\n")
	out, errs, code := ctl(t, "route", "apply", f, "--redis-addr", mr.Addr(), "--wait", "600ms")
	require.Equal(t, exitNotConverge, code)
	require.Contains(t, errs, "gw-1")
	require.NotContains(t, out, "is active on")
	_, _, code = ctl(t, "route", "status", "--redis-addr", mr.Addr())
	require.Equal(t, exitNotConverge, code)
}

func TestApplyRefusesGatewaysWithoutRouting(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	require.NoError(t, registryredis.New(rdb).Heartbeat(context.Background(),
		storage.GatewayInfo{ID: "gw-old", Boot: "b", Labels: map[string]string{"version": "v0"}}, time.Hour))
	f := write(t, "s.yaml", "rules:\n  - {name: canary, pool: canary}\n")
	_, errs, code := ctl(t, "route", "apply", f, "--redis-addr", mr.Addr())
	require.Equal(t, exitErr, code)
	require.Contains(t, errs, "gw-old")
	_, _, code = ctl(t, "route", "apply", f, "--redis-addr", mr.Addr(), "--force", "--wait", "0")
	require.Equal(t, 0, code)
}
