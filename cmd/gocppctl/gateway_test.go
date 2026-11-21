package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway/admin"
	"github.com/stretchr/testify/require"
)

// fakeGateway is an admin API that records what it is asked and updates the
// session count it reports in its lease.
type fakeGateway struct {
	mu       sync.Mutex
	sessions int
	softMax  int
	sheds    []admin.ShedRequest
	tokens   []string
	srv      *httptest.Server
}

func (f *fakeGateway) handler() http.Handler {
	mux := http.NewServeMux()
	st := func() admin.Status {
		return admin.Status{ID: "x", Sessions: f.sessions, SoftMax: f.softMax}
	}
	mux.HandleFunc("GET /admin/status", func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(st())
	})
	mux.HandleFunc("POST /admin/shed", func(w http.ResponseWriter, r *http.Request) {
		var req admin.ShedRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.tokens = append(f.tokens, r.Header.Get("Authorization"))
		f.sheds = append(f.sheds, req)
		f.sessions -= req.Count
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(st())
	})
	mux.HandleFunc("POST /admin/soft-max", func(w http.ResponseWriter, r *http.Request) {
		var body struct{ N int }
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		defer f.mu.Unlock()
		f.softMax = body.N
		_ = json.NewEncoder(w).Encode(st())
	})
	return mux
}

func setupFleet(t *testing.T, sessions ...int) (addr string, gws []*fakeGateway, beat func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	reg := registryredis.New(rdb)
	for _, n := range sessions {
		f := &fakeGateway{sessions: n}
		f.srv = httptest.NewServer(f.handler())
		t.Cleanup(f.srv.Close)
		gws = append(gws, f)
	}
	beat = func() {
		for i, f := range gws {
			f.mu.Lock()
			n := f.sessions
			f.mu.Unlock()
			require.NoError(t, reg.Heartbeat(context.Background(), storage.GatewayInfo{
				ID: "gw-" + strconv.Itoa(i), Boot: "b", State: storage.GatewayServing, Sessions: n,
				Labels: map[string]string{"version": "v" + strconv.Itoa(i), "admin": strings.TrimPrefix(f.srv.URL, "http://"), "policy_rev": "3"},
			}, time.Hour))
		}
	}
	beat()
	return mr.Addr(), gws, beat
}

func TestGatewayStatusShedAndCap(t *testing.T) {
	addr, gws, _ := setupFleet(t, 90, 10)
	out, errs, code := ctl(t, "gateway", "status", "--redis-addr", addr, "--live")
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "gw-0")
	require.Contains(t, out, "90.0%")
	require.Contains(t, out, "sessions=90")
	require.Contains(t, out, "2 gateways, 100 sessions, fair share 50.0")

	out, errs, code = ctl(t, "gateway", "shed", "--redis-addr", addr, "--gateway", "gw-0", "--count", "20",
		"--min-age", "15m", "--attr", "site=a", "--rate", "50", "--admin-token", "tok")
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "gw-0: shedding 20 of")
	gws[0].mu.Lock()
	require.Len(t, gws[0].sheds, 1)
	s := gws[0].sheds[0]
	require.Equal(t, 20, s.Count)
	require.Equal(t, admin.Duration(15*time.Minute), s.MinAge)
	require.Equal(t, map[string]string{"site": "a"}, s.Attrs)
	require.Equal(t, 50.0, s.Rate)
	require.Equal(t, []string{"Bearer tok"}, gws[0].tokens)
	gws[0].mu.Unlock()
	require.Empty(t, gws[1].sheds)

	_, errs, code = ctl(t, "gateway", "shed", "--redis-addr", addr, "--gateway", "nope", "--count", "1")
	require.Equal(t, exitErr, code)
	require.Contains(t, errs, "gw-0, gw-1")
	_, _, code = ctl(t, "gateway", "shed", "--redis-addr", addr, "--gateway", "gw-0")
	require.Equal(t, exitErr, code)

	// canary cap as a share of the fleet (100 sessions): 5% = 5
	out, errs, code = ctl(t, "gateway", "cap", "--redis-addr", addr, "--gateway", "gw-1", "--share", "5")
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "= 5")
	require.Equal(t, 5, gws[1].softMax)
	out, _, code = ctl(t, "gateway", "cap", "--redis-addr", addr, "--gateway", "gw-1", "--off")
	require.Equal(t, 0, code)
	require.Contains(t, out, "soft cap removed")
	require.Zero(t, gws[1].softMax)
	_, _, code = ctl(t, "gateway", "cap", "--redis-addr", addr, "--gateway", "gw-1")
	require.Equal(t, exitErr, code)
}

func TestGatewayRebalance(t *testing.T) {
	addr, gws, beat := setupFleet(t, 300, 300, 0)
	out, errs, code := ctl(t, "gateway", "rebalance", "--redis-addr", addr, "--dry-run")
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, "shed 100 from gw-0")
	require.Contains(t, out, "shed 100 from gw-1")
	require.Empty(t, gws[0].sheds, "dry run changes nothing")

	// Real run: the fake gateways drop the sessions; the chargers "reconnect" to gw-2
	// between the rounds (the test plays the load balancer).
	done := make(chan struct{})
	go func() {
		defer close(done)
		out, errs, code = ctl(t, "gateway", "rebalance", "--redis-addr", addr, "--settle", "300ms", "--rounds", "2", "--min-age", "1m")
	}()
	require.Eventually(t, func() bool {
		gws[0].mu.Lock()
		defer gws[0].mu.Unlock()
		return len(gws[0].sheds) == 1
	}, 5*time.Second, 20*time.Millisecond)
	gws[2].mu.Lock()
	gws[2].sessions = 200
	gws[2].mu.Unlock()
	beat()
	<-done
	require.Equal(t, 0, code, errs+out)
	require.Contains(t, out, "balanced")
	require.Equal(t, admin.Duration(time.Minute), gws[0].sheds[0].MinAge)
	require.Len(t, gws[0].sheds, 1, "balanced after the first round: no second shed")
}
