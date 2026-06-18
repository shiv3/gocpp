// Command csms-full is a fairly complete OCPP 1.6 CSMS built on gocpp, wiring the
// pluggable storage (transactions, config, connection registry), OpenTelemetry
// metrics and traces, structured logging, health endpoints, and graceful shutdown.
// It records a real charging session in the TransactionStore and (optionally)
// drives a connected charge point by sending RemoteStartTransaction after boot —
// enough to run the ocpp-cp-simulator scenarios end-to-end.
//
// Telemetry is unified on OpenTelemetry: when OTEL_EXPORTER_OTLP_ENDPOINT is set,
// both metrics and traces are exported via OTLP/HTTP (e.g. to an
// opentelemetry-collector). Without it, telemetry is a no-op.
//
// Run:
//
//	OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318 go run ./examples/csms-full
//
// Then drive it with the simulator, e.g.:
//
//	ocpp-cp-sim --cp-id CP_1 --connectors 1 \
//	  --ws-url ws://localhost:8080/ocpp/ \
//	  --scenario-template-file ../ocpp-cp-simulator/docs/examples/scenarios/demo-charging.json \
//	  --scenario-connector all
package main

import (
	"context"
	"errors"
	"io"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	otelmetrics "github.com/shiv3/gocpp/core/observability/metrics/otel"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/core/storage/memory"
	"github.com/shiv3/gocpp/csms"
	v16client "github.com/shiv3/gocpp/v16/client"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Telemetry: unified on OpenTelemetry. Both metrics and traces export via OTLP
	// when an endpoint is configured; otherwise they are no-ops.
	var metrics observability.Metrics = observability.NoOp{}
	var tracerProvider trace.TracerProvider
	var shutdownOTel func(context.Context) error
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") != "" {
		tp, m, shutdown, err := setupOTel(ctx)
		if err != nil {
			log.Fatalf("otel setup: %v", err)
		}
		metrics, tracerProvider, shutdownOTel = m, tp, shutdown
		logger.Info("OpenTelemetry enabled", "endpoint", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	}

	app := &app{
		logger:   logger,
		txStore:  memory.NewTransactionStore(),
		cfgStore: memory.NewConfigStore(),
		connReg:  memory.NewConnectionRegistry(),
		// autoRemoteStart drives the demo scenarios that wait for RemoteStartTransaction.
		autoRemoteStart: os.Getenv("AUTO_REMOTE_START") != "false",
	}

	opts := []csms.Option{
		csms.WithSubProtocols("ocpp2.1", "ocpp2.0.1", "ocpp1.6"),
		csms.WithLogger(logger),
		csms.WithMetrics(metrics),
		csms.WithConnectionRegistry(app.connReg),
		csms.WithTransactionStore(app.txStore),
		csms.WithConfigStore(app.cfgStore),
	}
	if tracerProvider != nil {
		opts = append(opts, csms.WithTracerProvider(tracerProvider))
	}
	srv := csms.NewServer(opts...)
	app.srv = srv
	app.registerHandlers()

	// Ops endpoints: health/readiness + an admin hook to originate CSMS->CP calls
	// (telemetry leaves via OTLP, not a scrape).
	ops := http.NewServeMux()
	ops.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	ops.HandleFunc("/readyz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	// POST /admin/call?cp=<cpId>&action=<Action> with a JSON request body sends a
	// CSMS-initiated operation (Reset, ChangeConfiguration, ...) to a connected CP
	// and returns the raw response — used to drive interop tests of CSMS->CP ops.
	ops.HandleFunc("/admin/call", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}
		cpID, action := r.URL.Query().Get("cp"), r.URL.Query().Get("action")
		conn, ok := srv.Get(cpID)
		if !ok {
			http.Error(w, "unknown cp: "+cpID, http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if len(body) == 0 {
			body = []byte("{}")
		}
		cctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer cancel()
		resp, err := csms.CallRaw(cctx, conn, action, body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(resp)
	})
	go func() { _ = http.ListenAndServe(envOr("OPS_ADDR", ":9090"), ops) }()

	httpSrv := &http.Server{Addr: envOr("ADDR", ":8080"), Handler: srv.Handler()}
	go func() {
		logger.Info("CSMS listening", "addr", httpSrv.Addr, "path", "/ocpp/")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatal(err)
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	srv.Close()
	if shutdownOTel != nil {
		_ = shutdownOTel(shutdownCtx)
	}
}

// setupOTel wires OTLP/HTTP exporters for both traces and metrics, returning a
// TracerProvider, a gocpp metrics adapter, and a combined shutdown func. The
// exporters read OTEL_EXPORTER_OTLP_ENDPOINT (an http:// URL implies insecure).
func setupOTel(ctx context.Context) (trace.TracerProvider, observability.Metrics, func(context.Context) error, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String("service.name", envOr("OTEL_SERVICE_NAME", "gocpp-csms-full")),
	))
	if err != nil {
		return nil, nil, nil, err
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp, sdkmetric.WithInterval(2*time.Second))),
		sdkmetric.WithResource(res),
	)

	m, err := otelmetrics.New(mp)
	if err != nil {
		return nil, nil, nil, err
	}

	shutdown := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}
	return tp, m, shutdown, nil
}

type app struct {
	// Embedding UnimplementedCSMSHandler makes *app a complete v16h.CSMSHandler:
	// the messages whose On* methods are defined below are handled, and every
	// other CP->CSMS message returns a NotSupported CallError.
	v16h.UnimplementedCSMSHandler

	logger          *slog.Logger
	srv             *csms.Server
	txStore         storage.TransactionStore
	cfgStore        storage.ConfigStore
	connReg         storage.ConnectionRegistry
	autoRemoteStart bool
	txCounter       atomic.Int32
}

// idTagAccepted/idTagInvalid are the canned authorization results shared by the
// Authorize/StartTransaction/StopTransaction handlers.
var (
	idTagAccepted = v16msg.IDTagInfo{Status: v16msg.IDTagInfoStatusAccepted}
	idTagInvalid  = v16msg.IDTagInfo{Status: v16msg.IDTagInfoStatusInvalid}
)

// registerHandlers wires every CP->CSMS handler *app implements in one call.
// ChangeConfiguration/GetConfiguration are SentByCSMS (CSMS -> CP); the CSMS
// *sends* those (via v16client.NewCSMS(conn)), so they are not handlers here.
func (a *app) registerHandlers() {
	must(v16h.RegisterCSMS(a.srv, a))
}

func (a *app) OnBootNotification(ctx context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
	a.logger.Info("BootNotification", "cp", c.ID(), "vendor", req.ChargePointVendor, "model", req.ChargePointModel)
	if a.autoRemoteStart {
		go a.driveRemoteStart(c)
	}
	return v16msg.BootNotificationResponse{CurrentTime: time.Now().UTC(), Interval: 300, Status: v16msg.RegistrationStatusAccepted}, nil
}

func (a *app) OnHeartbeat(ctx context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
	a.logger.Info("Heartbeat", "cp", c.ID())
	return v16msg.HeartbeatResponse{CurrentTime: time.Now().UTC()}, nil
}

func (a *app) OnStatusNotification(ctx context.Context, c *csms.Conn, req v16msg.StatusNotificationRequest) (v16msg.StatusNotificationResponse, error) {
	a.logger.Info("StatusNotification", "cp", c.ID(), "connector", req.ConnectorID, "status", req.Status)
	return v16msg.StatusNotificationResponse{}, nil
}

func (a *app) OnAuthorize(ctx context.Context, c *csms.Conn, req v16msg.AuthorizeRequest) (v16msg.AuthorizeResponse, error) {
	a.logger.Info("Authorize", "cp", c.ID(), "idTag", req.IDTag)
	if !knownIDTag(req.IDTag) {
		return v16msg.AuthorizeResponse{IDTagInfo: idTagInvalid}, nil
	}
	return v16msg.AuthorizeResponse{IDTagInfo: idTagAccepted}, nil
}

func (a *app) OnStartTransaction(ctx context.Context, c *csms.Conn, req v16msg.StartTransactionRequest) (v16msg.StartTransactionResponse, error) {
	txID := a.txCounter.Add(1)
	err := a.txStore.Begin(ctx, storage.Transaction{
		ID:         itoa(txID),
		CPID:       c.ID(),
		EVSEID:     int(req.ConnectorID),
		IDTag:      req.IDTag,
		StartedAt:  time.Now().UTC(),
		MeterStart: int(req.MeterStart),
		Status:     storage.TransactionActive,
	})
	if err != nil {
		a.logger.Error("tx begin", "err", err)
	}
	a.logger.Info("StartTransaction", "cp", c.ID(), "tx", txID, "idTag", req.IDTag, "meterStart", req.MeterStart)
	return v16msg.StartTransactionResponse{TransactionID: txID, IDTagInfo: idTagAccepted}, nil
}

func (a *app) OnMeterValues(ctx context.Context, c *csms.Conn, req v16msg.MeterValuesRequest) (v16msg.MeterValuesResponse, error) {
	if req.TransactionID != nil && len(req.MeterValue) > 0 {
		a.logger.Info("MeterValues", "cp", c.ID(), "tx", *req.TransactionID, "samples", len(req.MeterValue))
	}
	return v16msg.MeterValuesResponse{}, nil
}

func (a *app) OnStopTransaction(ctx context.Context, c *csms.Conn, req v16msg.StopTransactionRequest) (v16msg.StopTransactionResponse, error) {
	txID := itoa(req.TransactionID)
	_ = a.txStore.End(ctx, txID, storage.TransactionEnd{EndedAt: time.Now().UTC(), MeterStop: int(req.MeterStop), Status: storage.TransactionCompleted})
	a.logger.Info("StopTransaction", "cp", c.ID(), "tx", req.TransactionID, "meterStop", req.MeterStop)
	return v16msg.StopTransactionResponse{IDTagInfo: &idTagAccepted}, nil
}

func (a *app) OnDataTransfer(ctx context.Context, c *csms.Conn, req v16msg.DataTransferRequest) (v16msg.DataTransferResponse, error) {
	return v16msg.DataTransferResponse{Status: "Accepted"}, nil
}

func (a *app) OnDiagnosticsStatusNotification(ctx context.Context, c *csms.Conn, req v16msg.DiagnosticsStatusNotificationRequest) (v16msg.DiagnosticsStatusNotificationResponse, error) {
	a.logger.Info("DiagnosticsStatusNotification", "cp", c.ID(), "status", req.Status)
	return v16msg.DiagnosticsStatusNotificationResponse{}, nil
}

func (a *app) OnFirmwareStatusNotification(ctx context.Context, c *csms.Conn, req v16msg.FirmwareStatusNotificationRequest) (v16msg.FirmwareStatusNotificationResponse, error) {
	a.logger.Info("FirmwareStatusNotification", "cp", c.ID(), "status", req.Status)
	return v16msg.FirmwareStatusNotificationResponse{}, nil
}

func knownIDTag(idTag string) bool {
	switch idTag {
	case "CORE_VALID", "CORE_REMOTE", "TAG001", "TAG-A":
		return true
	default:
		return false
	}
}

// driveRemoteStart waits for the connection to settle, then sends a
// RemoteStartTransaction so scenarios that gate on it (e.g. demo-charging) proceed.
func (a *app) driveRemoteStart(c *csms.Conn) {
	time.Sleep(3 * time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	connector := int32(1)
	resp, err := v16client.NewCSMS(c).RemoteStartTransaction(ctx, v16msg.RemoteStartTransactionRequest{
		IDTag:       "TAG001",
		ConnectorID: &connector,
	})
	if err != nil {
		a.logger.Warn("RemoteStartTransaction failed", "cp", c.ID(), "err", err)
		return
	}
	a.logger.Info("RemoteStartTransaction", "cp", c.ID(), "status", resp.Status)
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func itoa(i int32) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b [12]byte
	pos := len(b)
	for i > 0 {
		pos--
		b[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		b[pos] = '-'
	}
	return string(b[pos:])
}
