package main

import (
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// probeFrames are the BootNotification payloads per subprotocol.
var probeBoot = map[string]string{
	"ocpp1.6":   `{"chargePointVendor":"gocpp-probe","chargePointModel":"probe"}`,
	"ocpp2.0.1": `{"reason":"PowerUp","chargingStation":{"model":"probe","vendorName":"gocpp-probe"}}`,
	"ocpp2.1":   `{"reason":"PowerUp","chargingStation":{"model":"probe","vendorName":"gocpp-probe"}}`,
}

// probe is the pre-flight check run as a startup probe: for each OCPP version
// it upgrades against the gateway's own listener with the reserved identity
// "$probe-<id>", sends BootNotification and Heartbeat and expects CALLRESULTs.
// A gateway that cannot complete this (no backend, bad TLS, broken link) never
// receives a charger. The backend must answer the probe identity without
// business side effects.
func probe(args []string) error {
	fs := flag.NewFlagSet("probe", flag.ContinueOnError)
	url := fs.String("url", env("PROBE_URL", "ws://127.0.0.1:8080/ocpp/"), "WebSocket URL prefix of the gateway's listener")
	id := fs.String("id", env("ID", "gateway"), "gateway id (identity is $probe-<id>)")
	versions := fs.String("versions", env("SUBPROTOCOLS", "ocpp1.6"), "comma separated subprotocols to check")
	timeout := fs.Duration("timeout", 10*time.Second, "overall timeout")
	insecure := fs.Bool("insecure", false, "do not verify the gateway's TLS certificate")
	interval := fs.Duration("interval", 0, "run continuously, every interval, until SIGTERM (a sidecar: an idle canary is still exercised); 0 = once")
	metricsAddr := fs.String("metrics", "", "with --interval: serve gocpp_probe_* metrics on this address")
	if err := fs.Parse(args); err != nil {
		return err
	}
	hc := &http.Client{}
	if *insecure {
		hc.Transport = &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}} //nolint:gosec // opt-in for self-signed pre-flight
	}
	run := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), *timeout)
		defer cancel()
		return probeAll(ctx, hc, *url, *id, *versions)
	}
	if *interval <= 0 {
		return run()
	}
	return probeLoop(*interval, *metricsAddr, *id, run)
}

func probeAll(ctx context.Context, hc *http.Client, url, id, versions string) error {
	for _, v := range strings.Split(versions, ",") {
		boot, ok := probeBoot[v]
		if !ok {
			return fmt.Errorf("probe: no BootNotification payload for %q", v)
		}
		if err := probeOne(ctx, hc, url+"$probe-"+id, v, boot); err != nil {
			return fmt.Errorf("probe %s: %w", v, err)
		}
	}
	return nil
}

// probeLoop runs the probe every interval until SIGINT/SIGTERM, logging each
// result and exporting gocpp_probe_runs_total{result}, the duration of the last
// run and the time of the last success. It never exits on a failed probe: a
// failing gateway is the signal, and the alert is on the metrics.
func probeLoop(interval time.Duration, metricsAddr, id string, run func() error) error {
	reg := prometheus.NewRegistry()
	runs := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "gocpp_probe_runs_total", Help: "synthetic probe runs by result"}, []string{"result"})
	dur := prometheus.NewGauge(prometheus.GaugeOpts{Name: "gocpp_probe_last_duration_seconds", Help: "duration of the last probe run"})
	okAt := prometheus.NewGauge(prometheus.GaugeOpts{Name: "gocpp_probe_last_success_timestamp_seconds", Help: "unix time of the last successful probe run"})
	reg.MustRegister(runs, dur, okAt)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	if metricsAddr != "" {
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
		srv := &http.Server{Addr: metricsAddr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
		go func() { _ = srv.ListenAndServe() }()
		defer func() { _ = srv.Close() }()
	}
	log := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		start := time.Now()
		err := run()
		dur.Set(time.Since(start).Seconds())
		if err != nil {
			runs.WithLabelValues("error").Inc()
			log.Error("probe failed", "gateway", id, "error", err)
		} else {
			runs.WithLabelValues("ok").Inc()
			okAt.SetToCurrentTime()
			log.Info("probe ok", "gateway", id, "seconds", time.Since(start).Seconds())
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

func probeOne(ctx context.Context, hc *http.Client, url, sub, boot string) error {
	c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{sub}, HTTPClient: hc})
	if err != nil {
		return fmt.Errorf("upgrade: %w", err)
	}
	defer func() { _ = c.CloseNow() }()
	for i, call := range []struct{ action, payload string }{{"BootNotification", boot}, {"Heartbeat", "{}"}} {
		msgID := fmt.Sprintf("probe-%d", i)
		frame := fmt.Sprintf(`[2,%q,%q,%s]`, msgID, call.action, call.payload)
		if err := c.Write(ctx, websocket.MessageText, []byte(frame)); err != nil {
			return fmt.Errorf("%s: %w", call.action, err)
		}
		_, data, err := c.Read(ctx)
		if err != nil {
			return fmt.Errorf("%s: %w", call.action, err)
		}
		if !strings.HasPrefix(string(data), `[3,"`+msgID+`"`) {
			return fmt.Errorf("%s: unexpected answer %s", call.action, data)
		}
	}
	return c.Close(websocket.StatusNormalClosure, "probe done")
}
