package main

import (
	"log/slog"

	"github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/observability/tap"
)

// newTap builds the frame/session tap exporter the --tap-* flags ask for, or nil
// when no sink is configured. Its counters are exported as
// gocpp_tap_events_total{result}.
func newTap(cfg config, reg prometheus.Registerer, log *slog.Logger) (*tap.Exporter, error) {
	var sinks []tap.Sink
	if cfg.TapJSONL != "" {
		s, err := tap.JSONLFile(cfg.TapJSONL)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, s)
	}
	if cfg.TapNATS {
		nc, err := nats.Connect(cfg.NATSURL, append(natsSecurityOptions(cfg), nats.Name("gocpp-gateway-tap-"+cfg.ID), nats.ReconnectBufSize(-1),
			nats.MaxReconnects(-1))...)
		if err != nil {
			return nil, err
		}
		sinks = append(sinks, closeWith(linknats.NewTapSink(nc, linknats.WithTapPrefix(cfg.NATSPrefix)), nc.Close))
	}
	if len(sinks) == 0 {
		return nil, nil
	}
	opts := []tap.Option{
		tap.WithSource(cfg.ID), tap.WithBuffer(cfg.TapBuffer), tap.WithMaxFrame(cfg.TapMaxFrame),
		tap.WithFrameSampling(cfg.TapSample),
	}
	if cfg.TapSkipHB {
		opts = append(opts, tap.WithFrameFilter(func(ev observability.FrameEvent) bool { return ev.Action != "Heartbeat" }))
	}
	ex := tap.New(tap.Multi(sinks...), opts...)
	reg.MustRegister(tapCollector{ex})
	log.Info("tap enabled", "jsonl", cfg.TapJSONL, "nats", cfg.TapNATS, "sample", cfg.TapSample,
		"frames", !cfg.TapNoFrames, "sessions", !cfg.TapNoSessions)
	return ex, nil
}

// closeWith closes after the sink: the NATS sink flushes but does not own its connection.
type closingSink struct {
	tap.Sink
	then func()
}

func closeWith(s tap.Sink, then func()) tap.Sink { return closingSink{s, then} }

func (c closingSink) Close() error {
	err := c.Sink.Close()
	c.then()
	return err
}

type tapCollector struct{ e *tap.Exporter }

var tapDesc = prometheus.NewDesc("gocpp_tap_events_total", "frame/session tap events by result (exported, dropped, sampled, failed)", []string{"result"}, nil)
var tapQueueDesc = prometheus.NewDesc("gocpp_tap_queue_depth", "events waiting for the tap sinks", nil, nil)

func (tapCollector) Describe(ch chan<- *prometheus.Desc) { ch <- tapDesc; ch <- tapQueueDesc }

func (c tapCollector) Collect(ch chan<- prometheus.Metric) {
	st := c.e.Stats()
	for result, v := range map[string]uint64{"exported": st.Exported, "dropped": st.Dropped, "sampled": st.Sampled, "failed": st.Failed} {
		ch <- prometheus.MustNewConstMetric(tapDesc, prometheus.CounterValue, float64(v), result)
	}
	ch <- prometheus.MustNewConstMetric(tapQueueDesc, prometheus.GaugeValue, float64(st.Queued))
}
