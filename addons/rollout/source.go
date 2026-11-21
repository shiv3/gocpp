package rollout

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// PoolStats is what the gates need to know about one pool over a window.
type PoolStats struct {
	// Requests is the number of inbound CALLs dispatched to the pool.
	Requests float64
	// Failed counts the failed outcomes: timeout, stream_broken and
	// callerror_InternalError.
	Failed float64
	// ByOutcome counts requests per outcome (at least the callerror_* ones).
	ByOutcome map[string]float64
	// P99 is the 99th percentile of the upstream latency in seconds; HasLatency is
	// false when there were no observations.
	P99        float64
	HasLatency bool
	// LateReplies and OrphanResults are the counts of gocpp_gateway_late_replies_total
	// and gocpp_gateway_orphan_results_total of the pool over the window (0 when
	// the gateways do not export them).
	LateReplies   float64
	OrphanResults float64
}

// Source supplies the metrics of a rollout.
type Source interface {
	// Stats returns the statistics of pool over the last window.
	Stats(ctx context.Context, pool string, window time.Duration) (PoolStats, error)
	// Scalar evaluates an application-level check query (see Check). A Source
	// that cannot returns ErrNoScalar.
	Scalar(ctx context.Context, query string) (float64, error)
}

// ErrNoScalar is returned by sources that cannot evaluate Check queries.
var ErrNoScalar = errors.New("rollout: this metrics source cannot evaluate check queries (use PromSource)")

// Sampler is implemented by sources that need to observe the metrics over time
// (ScrapeSource); the controller calls Sample before every evaluation and when a
// stage starts.
type Sampler interface {
	Sample(ctx context.Context) error
}

// ---- Prometheus ---------------------------------------------------------------

// PromSource evaluates the gate queries on a Prometheus server (HTTP API v1).
type PromSource struct {
	URL    string
	Client *http.Client // default: 15 s timeout
	// Token, when set, is sent as a bearer token.
	Token string
	// Now replaces time.Now (tests).
	Now func() time.Time
}

var _ Source = (*PromSource)(nil)

func promWindow(w time.Duration) string {
	s := int(math.Max(w.Seconds(), 1))
	return strconv.Itoa(s) + "s"
}

// Stats implements Source with the queries of docs/design/gateway.md 5.4 over
// gocpp_gateway_upstream_requests_total and gocpp_gateway_upstream_seconds.
func (p *PromSource) Stats(ctx context.Context, pool string, window time.Duration) (PoolStats, error) {
	w := promWindow(window)
	sel := fmt.Sprintf(`pool=%q`, pool)
	var st PoolStats
	var err error
	if st.Requests, err = p.scalar(ctx, fmt.Sprintf(`sum(increase(gocpp_gateway_upstream_requests_total{%s}[%s]))`, sel, w)); err != nil {
		return st, err
	}
	if st.Failed, err = p.scalar(ctx, fmt.Sprintf(`sum(increase(gocpp_gateway_upstream_requests_total{%s,outcome=~"timeout|stream_broken|callerror_InternalError"}[%s]))`, sel, w)); err != nil {
		return st, err
	}
	vec, err := p.vector(ctx, fmt.Sprintf(`sum by (outcome) (increase(gocpp_gateway_upstream_requests_total{%s,outcome=~"callerror_.*"}[%s]))`, sel, w))
	if err != nil {
		return st, err
	}
	st.ByOutcome = map[string]float64{}
	for _, s := range vec {
		st.ByOutcome[s.labels["outcome"]] = s.value
	}
	if st.LateReplies, err = p.scalar(ctx, fmt.Sprintf(`sum(increase(gocpp_gateway_late_replies_total{%s}[%s]))`, sel, w)); err != nil {
		return st, err
	}
	if st.OrphanResults, err = p.scalar(ctx, fmt.Sprintf(`sum(increase(gocpp_gateway_orphan_results_total{%s}[%s]))`, sel, w)); err != nil {
		return st, err
	}
	q, err := p.vector(ctx, fmt.Sprintf(`histogram_quantile(0.99, sum by (le) (rate(gocpp_gateway_upstream_seconds_bucket{%s}[%s])))`, sel, w))
	if err != nil {
		return st, err
	}
	if len(q) == 1 && !math.IsNaN(q[0].value) {
		st.P99, st.HasLatency = q[0].value, true
	}
	return st, nil
}

// Scalar implements Source.
func (p *PromSource) Scalar(ctx context.Context, query string) (float64, error) {
	return p.scalar(ctx, query)
}

type promSample struct {
	labels map[string]string
	value  float64
}

func (p *PromSource) scalar(ctx context.Context, q string) (float64, error) {
	v, err := p.vector(ctx, q)
	if err != nil {
		return 0, err
	}
	if len(v) == 0 {
		return 0, nil // no series: nothing happened
	}
	return v[0].value, nil
}

func (p *PromSource) vector(ctx context.Context, q string) ([]promSample, error) {
	now := time.Now
	if p.Now != nil {
		now = p.Now
	}
	form := url.Values{"query": {q}, "time": {strconv.FormatInt(now().Unix(), 10)}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(p.URL, "/")+"/api/v1/query", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	cl := p.Client
	if cl == nil {
		cl = &http.Client{Timeout: 15 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var out struct {
		Status string `json:"status"`
		Error  string `json:"error"`
		Data   struct {
			Result []struct {
				Metric map[string]string `json:"metric"`
				Value  [2]any            `json:"value"`
			} `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("rollout: prometheus: %s: %w", resp.Status, err)
	}
	if out.Status != "success" {
		return nil, fmt.Errorf("rollout: prometheus: %s", out.Error)
	}
	var res []promSample
	for _, r := range out.Data.Result {
		s, _ := r.Value[1].(string)
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("rollout: prometheus value %q: %w", s, err)
		}
		res = append(res, promSample{labels: r.Metric, value: v})
	}
	return res, nil
}

// ---- helpers shared by the sources ----------------------------------------------

func isFailed(outcome string) bool {
	return outcome == "timeout" || outcome == "stream_broken" || outcome == "callerror_InternalError"
}

// bucket is one cumulative histogram bucket.
type bucket struct {
	le    float64
	count float64
}

// quantile estimates q from cumulative buckets by linear interpolation (the same
// method as histogram_quantile). ok is false when there are no observations.
func quantile(q float64, bs []bucket) (float64, bool) {
	sort.Slice(bs, func(i, j int) bool { return bs[i].le < bs[j].le })
	if len(bs) < 2 || bs[len(bs)-1].count <= 0 {
		return 0, false
	}
	total := bs[len(bs)-1].count
	rank := q * total
	prevLE, prevC := 0.0, 0.0
	for _, b := range bs {
		if b.count >= rank {
			if math.IsInf(b.le, 1) {
				return prevLE, true
			}
			if b.count == prevC {
				return b.le, true
			}
			return prevLE + (b.le-prevLE)*(rank-prevC)/(b.count-prevC), true
		}
		prevLE, prevC = b.le, b.count
	}
	return prevLE, true
}
