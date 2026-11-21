package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

const testPlan = `
rule: step
base:
  rules:
    - {name: step, pool: canary}
stages:
  - {percent: 10, soak: 100ms}
  - {percent: 100, soak: 100ms}
interval: 100ms
window: 1s
gates: {min_requests: 10}
`

func TestRolloutValidateAndDryRun(t *testing.T) {
	p := write(t, "plan.yaml", testPlan)
	out, errs, code := ctl(t, "rollout", "validate", p)
	require.Equal(t, 0, code, errs)
	require.Contains(t, out, `ok: rule "step" through 2 stages`)
	_, errs, code = ctl(t, "rollout", "validate", write(t, "bad.yaml", "rule: nope\nbase: {}\nstages: []\n"))
	require.Equal(t, exitErr, code)
	require.Contains(t, errs, "not in the base policy")

	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		v := "100"
		if strings.Contains(string(body), "outcome%3D~%22timeout") || strings.Contains(string(body), "sum+by+%28outcome") {
			v = "0"
		}
		res := []map[string]any{{"metric": map[string]string{}, "value": []any{1.0, v}}}
		if strings.Contains(string(body), "histogram_quantile") {
			res = nil
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "success", "data": map[string]any{"resultType": "vector", "result": res}})
	}))
	defer prom.Close()
	mr := miniredis.RunT(t)
	out, errs, code = ctl(t, "rollout", "run", p, "--dry-run", "--redis-addr", mr.Addr(), "--prometheus", prom.URL)
	require.Equal(t, 0, code, out+errs)
	require.Contains(t, out, "dry run")
	require.Contains(t, out, "would apply stage")
	require.Contains(t, out, "ready to promote")
	// nothing was written
	out, _, _ = ctl(t, "route", "show", "--redis-addr", mr.Addr())
	require.Contains(t, out, "no policy stored")
}
