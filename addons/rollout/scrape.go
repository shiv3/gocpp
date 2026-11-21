package rollout

import (
	"context"
	"fmt"
	"io"
	"math"
	"net/http"
	"sync"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
)

// ScrapeSource reads the gateways' own /metrics endpoints, so a rollout can run
// without a Prometheus server. It keeps the counters of each scrape and answers
// Stats from the difference between now and the oldest scrape inside the window,
// per gateway (a restarted gateway's counter reset is treated as starting from
// zero). The controller drives it through Sampler: the first window of a stage
// has the stage start as its baseline.
type ScrapeSource struct {
	// Targets are the metrics URLs, e.g. http://gateway-0.gateway-link:9100/metrics.
	Targets []string
	Client  *http.Client // default: 10 s timeout
	Now     func() time.Time

	mu    sync.Mutex
	snaps []snapshot
}

var (
	_ Source  = (*ScrapeSource)(nil)
	_ Sampler = (*ScrapeSource)(nil)
)

type snapshot struct {
	at      time.Time
	targets map[string]*targetData
}

type targetData struct {
	requests map[string]map[string]float64  // pool -> outcome -> count
	buckets  map[string]map[float64]float64 // pool -> le -> cumulative count
	late     map[string]float64             // pool -> late replies
	orphans  map[string]float64             // pool -> orphan results
}

func (s *ScrapeSource) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Sample scrapes every target once. A target that cannot be read is skipped (the
// others still count); all failing is an error.
func (s *ScrapeSource) Sample(ctx context.Context) error {
	snap := snapshot{at: s.now(), targets: map[string]*targetData{}}
	var firstErr error
	for _, t := range s.Targets {
		d, err := s.scrape(ctx, t)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		snap.targets[t] = d
	}
	if len(snap.targets) == 0 && len(s.Targets) > 0 {
		return fmt.Errorf("rollout: no gateway could be scraped: %w", firstErr)
	}
	s.mu.Lock()
	s.snaps = append(s.snaps, snap)
	// Keep a day at most.
	cut := snap.at.Add(-24 * time.Hour)
	for len(s.snaps) > 1 && s.snaps[0].at.Before(cut) {
		s.snaps = s.snaps[1:]
	}
	s.mu.Unlock()
	return nil
}

func (s *ScrapeSource) scrape(ctx context.Context, target string) (*targetData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	cl := s.Client
	if cl == nil {
		cl = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s: %s", target, resp.Status)
	}
	return parseMetrics(io.LimitReader(resp.Body, 64<<20))
}

func labelOf(m *dto.Metric, name string) string {
	for _, l := range m.GetLabel() {
		if l.GetName() == name {
			return l.GetValue()
		}
	}
	return ""
}

func parseMetrics(r io.Reader) (*targetData, error) {
	p := expfmt.NewTextParser(model.UTF8Validation)
	fams, err := p.TextToMetricFamilies(r)
	if err != nil {
		return nil, err
	}
	d := &targetData{requests: map[string]map[string]float64{}, buckets: map[string]map[float64]float64{},
		late: map[string]float64{}, orphans: map[string]float64{}}
	for _, m := range fams["gocpp_gateway_late_replies_total"].GetMetric() {
		d.late[labelOf(m, "pool")] += m.GetCounter().GetValue()
	}
	for _, m := range fams["gocpp_gateway_orphan_results_total"].GetMetric() {
		d.orphans[labelOf(m, "pool")] += m.GetCounter().GetValue()
	}
	for _, m := range fams["gocpp_gateway_upstream_requests_total"].GetMetric() {
		pool, outcome := labelOf(m, "pool"), labelOf(m, "outcome")
		if d.requests[pool] == nil {
			d.requests[pool] = map[string]float64{}
		}
		d.requests[pool][outcome] += m.GetCounter().GetValue()
	}
	for _, m := range fams["gocpp_gateway_upstream_seconds"].GetMetric() {
		pool := labelOf(m, "pool")
		if d.buckets[pool] == nil {
			d.buckets[pool] = map[float64]float64{}
		}
		h := m.GetHistogram()
		for _, b := range h.GetBucket() {
			d.buckets[pool][b.GetUpperBound()] += float64(b.GetCumulativeCount())
		}
		d.buckets[pool][math.Inf(1)] += float64(h.GetSampleCount())
	}
	return d, nil
}

// Stats implements Source.
func (s *ScrapeSource) Stats(_ context.Context, pool string, window time.Duration) (PoolStats, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.snaps) < 2 {
		return PoolStats{}, nil // nothing to difference yet: no data, no decision
	}
	latest := s.snaps[len(s.snaps)-1]
	base := s.snaps[0]
	from := latest.at.Add(-window)
	for _, sn := range s.snaps {
		if !sn.at.Before(from) {
			base = sn
			break
		}
	}
	if base.at.Equal(latest.at) {
		return PoolStats{}, nil
	}
	st := PoolStats{ByOutcome: map[string]float64{}}
	les := map[float64]float64{}
	for tgt, now := range latest.targets {
		old := base.targets[tgt] // nil: the target is new, everything counts
		for outcome, v := range now.requests[pool] {
			d := v
			if old != nil {
				if o, ok := old.requests[pool][outcome]; ok && v >= o {
					d = v - o
				}
			}
			st.Requests += d
			if isFailed(outcome) {
				st.Failed += d
			}
			if len(outcome) > 10 && outcome[:10] == "callerror_" {
				st.ByOutcome[outcome] += d
			}
		}
		st.LateReplies += counterDelta(now.late[pool], old, func(d *targetData) (float64, bool) { v, ok := d.late[pool]; return v, ok })
		st.OrphanResults += counterDelta(now.orphans[pool], old, func(d *targetData) (float64, bool) { v, ok := d.orphans[pool]; return v, ok })
		for le, v := range now.buckets[pool] {
			d := v
			if old != nil {
				if o, ok := old.buckets[pool][le]; ok && v >= o {
					d = v - o
				}
			}
			les[le] += d
		}
	}
	var bs []bucket
	for le, c := range les {
		bs = append(bs, bucket{le: le, count: c})
	}
	st.P99, st.HasLatency = quantile(0.99, bs)
	return st, nil
}

// counterDelta is v minus the older value of the same series; a series that is
// new or went backwards (a restarted gateway) counts from zero.
func counterDelta(v float64, old *targetData, get func(*targetData) (float64, bool)) float64 {
	if old != nil {
		if o, ok := get(old); ok && v >= o {
			return v - o
		}
	}
	return v
}

// Scalar implements Source.
func (s *ScrapeSource) Scalar(context.Context, string) (float64, error) { return 0, ErrNoScalar }
