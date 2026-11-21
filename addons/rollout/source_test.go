package rollout_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/addons/rollout"
	"github.com/stretchr/testify/require"
)

// metricsText renders the two gateway metric families the sources read.
func metricsText(req map[string]float64, lat map[string][]float64) string {
	var b strings.Builder
	b.WriteString("# TYPE gocpp_gateway_upstream_requests_total counter\n")
	for k, v := range req { // k = pool/outcome
		pool, outcome, _ := strings.Cut(k, "/")
		fmt.Fprintf(&b, "gocpp_gateway_upstream_requests_total{pool=%q,backend_version=\"v\",action=\"Heartbeat\",outcome=%q} %g\n", pool, outcome, v)
	}
	b.WriteString("# TYPE gocpp_gateway_upstream_seconds histogram\n")
	les := []string{"0.01", "0.05", "0.1", "+Inf"}
	for pool, counts := range lat { // cumulative counts per bucket
		for i, le := range les {
			fmt.Fprintf(&b, "gocpp_gateway_upstream_seconds_bucket{pool=%q,backend_version=\"v\",action=\"Heartbeat\",le=%q} %g\n", pool, le, counts[i])
		}
		fmt.Fprintf(&b, "gocpp_gateway_upstream_seconds_count{pool=%q,backend_version=\"v\",action=\"Heartbeat\"} %g\n", pool, counts[len(counts)-1])
		fmt.Fprintf(&b, "gocpp_gateway_upstream_seconds_sum{pool=%q,backend_version=\"v\",action=\"Heartbeat\"} 1\n", pool)
	}
	return b.String()
}

func TestScrapeSourceDifferencesCounters(t *testing.T) {
	var mu sync.Mutex
	var bodies [2]string
	srvs := make([]*httptest.Server, 2)
	for i := range srvs {
		srvs[i] = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			mu.Lock()
			defer mu.Unlock()
			_, _ = io.WriteString(w, bodies[i])
		}))
		defer srvs[i].Close()
	}
	set := func(i int, req map[string]float64, lat map[string][]float64) {
		mu.Lock()
		bodies[i] = metricsText(req, lat)
		mu.Unlock()
	}
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	src := &rollout.ScrapeSource{Targets: []string{srvs[0].URL, srvs[1].URL}, Now: func() time.Time { return now }}

	// baseline (counters from before the stage must not count)
	set(0, map[string]float64{"canary/result": 1000, "canary/callerror_InternalError": 10}, map[string][]float64{"canary": {1000, 1000, 1000, 1000}})
	set(1, map[string]float64{"canary/result": 500}, map[string][]float64{"canary": {500, 500, 500, 500}})
	require.NoError(t, src.Sample(context.Background()))
	st, err := src.Stats(context.Background(), "canary", time.Minute)
	require.NoError(t, err)
	require.Zero(t, st.Requests, "one scrape: no difference yet")

	now = now.Add(time.Minute)
	// gw-0 serves 600 more (30 failed, 20 FormationViolation); gw-1 restarted (counter reset) and served 200.
	set(0, map[string]float64{"canary/result": 1550, "canary/callerror_InternalError": 40, "canary/callerror_FormationViolation": 20},
		map[string][]float64{"canary": {1000, 1400, 1610, 1610}})
	set(1, map[string]float64{"canary/result": 200}, map[string][]float64{"canary": {0, 180, 200, 200}})
	require.NoError(t, src.Sample(context.Background()))
	st, err = src.Stats(context.Background(), "canary", time.Minute)
	require.NoError(t, err)
	require.InDelta(t, 600+200+0, st.Requests, 0.01) // 550+30+20 = 600 on gw-0, 200 on gw-1
	require.InDelta(t, 30, st.Failed, 0.01)
	require.InDelta(t, 20, st.ByOutcome["callerror_FormationViolation"], 0.01)
	require.True(t, st.HasLatency)
	require.Greater(t, st.P99, 0.05)
	require.LessOrEqual(t, st.P99, 0.1)

	other, err := src.Stats(context.Background(), "default", time.Minute)
	require.NoError(t, err)
	require.Zero(t, other.Requests)
	_, err = src.Scalar(context.Background(), "x")
	require.ErrorIs(t, err, rollout.ErrNoScalar)

	// An unreachable target is skipped; all unreachable is an error.
	srvs[1].Close()
	require.NoError(t, src.Sample(context.Background()))
	dead := &rollout.ScrapeSource{Targets: []string{"http://127.0.0.1:1/metrics"}}
	require.Error(t, dead.Sample(context.Background()))
}

func TestPromSourceQueries(t *testing.T) {
	var queries []string
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		q := r.PostForm.Get("query")
		mu.Lock()
		queries = append(queries, q)
		mu.Unlock()
		require.Equal(t, "Bearer tok", r.Header.Get("Authorization"))
		vec := func(v string, labels map[string]string) map[string]any {
			return map[string]any{"metric": labels, "value": []any{1.0, v}}
		}
		var res []map[string]any
		switch {
		case strings.Contains(q, "histogram_quantile"):
			res = []map[string]any{vec("0.045", nil)}
		case strings.Contains(q, "sum by (outcome)"):
			res = []map[string]any{vec("7", map[string]string{"outcome": "callerror_FormationViolation"})}
		case strings.Contains(q, "outcome=~\"timeout"):
			res = []map[string]any{vec("3", nil)}
		case q == "absent_metric":
			res = nil
		default:
			res = []map[string]any{vec("1000", nil)}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": res}})
	}))
	defer srv.Close()
	src := &rollout.PromSource{URL: srv.URL, Token: "tok"}
	st, err := src.Stats(context.Background(), "canary", 5*time.Minute)
	require.NoError(t, err)
	require.Equal(t, 1000.0, st.Requests)
	require.Equal(t, 3.0, st.Failed)
	require.Equal(t, map[string]float64{"callerror_FormationViolation": 7}, st.ByOutcome)
	require.True(t, st.HasLatency)
	require.InDelta(t, 0.045, st.P99, 1e-9)
	mu.Lock()
	require.Contains(t, queries[0], `pool="canary"`)
	require.Contains(t, queries[0], "[300s]")
	mu.Unlock()
	v, err := src.Scalar(context.Background(), "absent_metric")
	require.NoError(t, err)
	require.Zero(t, v, "no series reads as zero")

	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"status":"error","error":"parse error"}`)
	}))
	defer bad.Close()
	_, err = (&rollout.PromSource{URL: bad.URL}).Stats(context.Background(), "canary", time.Minute)
	require.ErrorContains(t, err, "parse error")
}

func TestKubeEvents(t *testing.T) {
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/v1/namespaces/prod/events", r.URL.Path)
		require.Equal(t, "Bearer sa", r.Header.Get("Authorization"))
		var ev map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&ev))
		got = append(got, ev)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()
	k := &rollout.KubeEvents{API: srv.URL, Namespace: "prod", Token: "sa", Object: rollout.ObjectRef{Kind: "Deployment", Name: "backend-canary"}}
	k.Notify(rollout.Decision{Action: rollout.ActionHold, Reason: "soaking"})
	k.Notify(rollout.Decision{Action: rollout.ActionRollback, Stage: 1, Percent: 10, Reason: "gate failed", DryRun: true})
	require.Len(t, got, 1, "holds are not events")
	require.Equal(t, "Warning", got[0]["type"])
	require.Equal(t, "RolloutRollback", got[0]["reason"])
	require.Contains(t, got[0]["message"], "[dry run]")
	require.Equal(t, "backend-canary", got[0]["involvedObject"].(map[string]any)["name"])
}
