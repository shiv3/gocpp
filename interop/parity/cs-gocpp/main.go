// Command cs-gocpp is the gocpp half of the parity test CSMS pair.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"log/slog"
	"math/rand/v2"
	"net/http"
	_ "net/http/pprof"
	"os"
	"os/signal"
	"runtime/debug"
	"strings"
	"syscall"
	"time"

	"github.com/redis/go-redis/v9"
	linkgrpc "github.com/shiv3/gocpp/addons/link-grpc"
	registryredis "github.com/shiv3/gocpp/addons/registry-redis"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/interop/parity/common"
	v16 "github.com/shiv3/gocpp/v16"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	listen := flag.String("listen", "", "listen address, e.g. 127.0.0.1:9000 (required)")
	eventsPath := flag.String("events", "", "append events as JSONL to this file")
	callTimeout := flag.Duration("call-timeout", 10*time.Second, "timeout for CSMS-initiated calls")
	schemaMode := flag.String("schema", "lenient", "schema validation mode: lenient|strict|tolerant|off")
	compat := flag.Bool("compat", false, "enable csms.WithOCPPGoCompat() (implies lenient schema)")
	firstWins := flag.Bool("first-wins", false, "csms.DuplicatePolicyRejectNewAfterUpgrade: a duplicate charge point id is upgraded then closed with 1008 (upstream ocpp-go v0.19.0; not in the compat preset)")
	unlimited := flag.Bool("unlimited-size", false, "csms.WithUnlimitedMessageSize: no inbound size limit (upstream ocpp-go v0.19.0; not in the compat preset)")
	lite := flag.Bool("lite", false, "load-test mode: keep no event history, only counters (see /control/count)")
	mode := flag.String("mode", "embedded", "embedded | backend (processing tier behind gocpp-gateway; -listen then serves only /control)")
	gateways := flag.String("gateways", "", "backend mode: comma separated gateway link addresses (host:port)")
	redisAddr := flag.String("redis", "", "backend mode: session registry (Redis) address")
	pool := flag.String("pool", "", "backend mode: backend pool name")
	beID := flag.String("id", "", "backend mode: backend id")
	pprofAddr := flag.String("pprof", "", "serve net/http/pprof on this address, e.g. 127.0.0.1:6060")
	handlerPool := flag.Int("handler-pool", 0, "run inbound handlers on N resident workers (csms.WithHandlerPool); 0 = goroutine per message")
	outboundValidation := flag.Bool("outbound-validation", true, "validate payloads produced by local handlers (csms.WithOutboundValidation)")
	drainWindow := flag.Duration("drain-window", 0, "on SIGTERM, Server.Drain over this window (new upgrades get 503, readiness at /ready fails) before exiting; 0 = exit at once")
	acceptRate := flag.Float64("accept-rate", 0, "csms.WithAcceptRateLimit rate per second (0 = off)")
	acceptBurst := flag.Int("accept-burst", 0, "burst for -accept-rate (default 2x the rate)")
	failRate := flag.Float64("fail-rate", 0, "load-test fault injection: answer this fraction of Heartbeat/MeterValues/StatusNotification with an InternalError CALLERROR (an error-injecting canary)")
	handledLog := flag.String("handled-log", "", "append one line `unix_ms charger action` per handled Heartbeat/MeterValues to this file (routing correctness checks)")
	linkLinger := flag.Duration("link-write-linger", 0, "backend mode: linkgrpc.WithBackendWriteLinger (reply frames gather for up to this long on a busy link; 0 = off)")
	flag.Parse()
	if *pprofAddr != "" {
		go func() { log.Println(http.ListenAndServe(*pprofAddr, nil)) }()
	}
	if *listen == "" {
		log.Fatal("-listen is required")
	}

	rec, err := common.NewRecorder(*eventsPath)
	if err != nil {
		log.Fatal(err)
	}
	rec.SetLite(*lite)

	logger := slog.New(&captureHandler{rec: rec})
	opts := []csms.Option{
		csms.WithSubProtocols("ocpp1.6"),
		csms.WithLogger(logger),
		csms.WithCallTimeout(*callTimeout),
		csms.WithOnConnect(func(c *csms.Conn) { rec.Connect(c.ID(), "") }),
		csms.WithOnDisconnect(func(c *csms.Conn, err error) {
			cause, raw := disconnectCause(err)
			rec.Disconnect(c.ID(), cause, raw)
		}),
	}
	if *handlerPool > 0 {
		opts = append(opts, csms.WithHandlerPool(dispatcher.NewHandlerPool(*handlerPool)))
	}
	if *acceptRate > 0 {
		burst := *acceptBurst
		if burst <= 0 {
			burst = int(2 * *acceptRate)
		}
		opts = append(opts, csms.WithAcceptRateLimit(*acceptRate, burst))
	}
	if !*outboundValidation {
		opts = append(opts, csms.WithOutboundValidation(false))
	}
	if *firstWins {
		opts = append(opts, csms.WithDuplicatePolicy(csms.DuplicatePolicyRejectNewAfterUpgrade))
	}
	if *unlimited {
		opts = append(opts, csms.WithUnlimitedMessageSize())
	}
	if *compat {
		// The preset sets lenient schema, the ocpp-go error codes, message id drop,
		// UTC whole-second times and the keepalive; it needs a registry.
		reg := schema.NewRegistry()
		if err := v16.RegisterSchemas(reg); err != nil {
			log.Fatal(err)
		}
		opts = append(opts, csms.WithSchemaRegistry(reg), csms.WithOCPPGoCompat())
		*schemaMode = "compat"
	}
	switch *schemaMode {
	case "compat":
	case "off":
	case "lenient", "strict", "tolerant":
		reg := schema.NewRegistry()
		if err := v16.RegisterSchemas(reg); err != nil {
			log.Fatal(err)
		}
		opts = append(opts, csms.WithSchemaRegistry(reg))
		switch *schemaMode {
		case "lenient":
			opts = append(opts, csms.WithLenientSchema())
		case "strict":
			opts = append(opts, csms.WithStrictSchema(true))
		case "tolerant":
			opts = append(opts, csms.WithTolerantSchema())
		}
	default:
		log.Fatalf("unknown -schema %q", *schemaMode)
	}
	var link *linkgrpc.Backend
	if *mode == "backend" {
		rec.AssumeConnected(true)
		link = linkgrpc.NewBackend(linkgrpc.StaticTargets(strings.Split(*gateways, ",")...), linkgrpc.WithBackendWriteLinger(*linkLinger),
			linkgrpc.WithDialOptions(grpc.WithTransportCredentials(insecure.NewCredentials())),
			linkgrpc.WithResolveInterval(500*time.Millisecond), linkgrpc.WithDrainTimeout(30*time.Second))
		opts = append(opts, csms.WithGatewayLink(link), csms.WithBackendInfo(gateway.BackendInfo{ID: *beID, Pool: *pool, Version: "loadtest"}))
		if *redisAddr != "" {
			opts = append(opts, csms.WithSessionRegistry(registryredis.New(redis.NewClient(&redis.Options{Addr: *redisAddr}))))
		}
	}
	srv := csms.NewServer(opts...)
	h := &handler{rec: rec, failRate: *failRate}
	if *handledLog != "" {
		hl, err := newHandledLog(*handledLog)
		if err != nil {
			log.Fatal(err)
		}
		h.hlog = hl
	}
	if err := v16h.RegisterCSMS(srv, h); err != nil {
		log.Fatal(err)
	}

	be := &backend{srv: srv}
	mux := http.NewServeMux()
	mux.Handle("/control/", common.Control(rec, be, *callTimeout))
	mux.Handle("/ready", srv.ReadyHandler())
	if *mode == "backend" {
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
		defer stop()
		go func() {
			// SIGTERM: drain the link (Nack new work, finish what is in flight).
			<-ctx.Done()
			time.Sleep(50 * time.Millisecond)
		}()
		hs := &http.Server{Addr: *listen, Handler: mux}
		go func() { log.Println(hs.ListenAndServe()) }()
		fmt.Fprintf(os.Stderr, "cs-gocpp backend (pool=%q) control on %s, gateways %s\n", *pool, *listen, *gateways)
		if err := srv.ServeGateway(ctx); err != nil {
			log.Println("drain:", err)
		}
		return
	}
	mux.Handle("/", srv.Handler())
	fmt.Fprintf(os.Stderr, "cs-gocpp listening on %s (schema=%s)\n", *listen, *schemaMode)
	if *drainWindow > 0 {
		go func() {
			sig := make(chan os.Signal, 1)
			signal.Notify(sig, syscall.SIGTERM)
			<-sig
			fmt.Fprintf(os.Stderr, "SIGTERM: draining over %s\n", *drainWindow)
			ctx, cancel := context.WithTimeout(context.Background(), *drainWindow+30*time.Second)
			defer cancel()
			err := srv.Drain(ctx, csms.DrainOptions{Window: *drainWindow, RetryAfter: 10 * time.Second})
			fmt.Fprintf(os.Stderr, "drain finished: %v\n", err)
			os.Exit(0)
		}()
	}
	log.Fatal((&http.Server{Addr: *listen, Handler: mux}).ListenAndServe())
}

// disconnectCause normalizes the error gocpp passes to the disconnect hook.
func disconnectCause(err error) (cause, raw string) {
	if err == nil {
		return common.CauseUnknown, ""
	}
	raw = err.Error()
	switch {
	case errors.Is(err, ocppj.ErrConnReplaced):
		return common.CauseReplaced, raw
	case errors.Is(err, ocppj.ErrConnClosedByServer), errors.Is(err, ocppj.ErrConnClosed):
		return common.CauseServerClosed, raw
	}
	return common.ClassifyReadError(raw), raw
}

// ---------------------------------------------------------------- handlers

// handler implements the canned CP->CSMS responses. Security-extension
// messages are left to UnimplementedCSMSHandler (NotSupported CallError).
type handler struct {
	v16h.UnimplementedCSMSHandler
	rec      *common.Recorder
	tx       common.TxCounter
	failRate float64
	hlog     *handledLog
}

// inject returns the injected InternalError for a fraction of the calls (see
// -fail-rate); it also writes the handled log.
func (h *handler) inject(id, action string) error {
	h.hlog.note(id, action)
	if h.failRate > 0 && (h.failRate >= 1 || rand.Float64() < h.failRate) {
		return ocppj.NewCallError(ocppj.ErrorCodeInternalError, "injected", nil)
	}
	return nil
}

func idTagInfo(idTag string) v16msg.IDTagInfo {
	return v16msg.IDTagInfo{Status: v16msg.IDTagInfoStatus(common.AuthStatus(idTag))}
}

func (h *handler) OnBootNotification(_ context.Context, c *csms.Conn, req v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
	h.rec.Request(c.ID(), "BootNotification", req)
	resp := v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, Interval: common.BootInterval, CurrentTime: common.FixedTimeT}
	h.rec.Response(c.ID(), "BootNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnHeartbeat(_ context.Context, c *csms.Conn, req v16msg.HeartbeatRequest) (v16msg.HeartbeatResponse, error) {
	h.rec.Request(c.ID(), "Heartbeat", req)
	if err := h.inject(c.ID(), "Heartbeat"); err != nil {
		return v16msg.HeartbeatResponse{}, err
	}
	resp := v16msg.HeartbeatResponse{CurrentTime: common.FixedTimeT}
	h.rec.Response(c.ID(), "Heartbeat", resp, nil)
	return resp, nil
}

func (h *handler) OnAuthorize(_ context.Context, c *csms.Conn, req v16msg.AuthorizeRequest) (v16msg.AuthorizeResponse, error) {
	h.rec.Request(c.ID(), "Authorize", req)
	resp := v16msg.AuthorizeResponse{IDTagInfo: idTagInfo(req.IDTag)}
	h.rec.Response(c.ID(), "Authorize", resp, nil)
	return resp, nil
}

func (h *handler) OnStartTransaction(_ context.Context, c *csms.Conn, req v16msg.StartTransactionRequest) (v16msg.StartTransactionResponse, error) {
	h.rec.Request(c.ID(), "StartTransaction", req)
	resp := v16msg.StartTransactionResponse{IDTagInfo: idTagInfo(req.IDTag), TransactionID: int64(h.tx.Next())}
	h.rec.Response(c.ID(), "StartTransaction", resp, nil)
	return resp, nil
}

func (h *handler) OnStopTransaction(_ context.Context, c *csms.Conn, req v16msg.StopTransactionRequest) (v16msg.StopTransactionResponse, error) {
	h.rec.Request(c.ID(), "StopTransaction", req)
	var resp v16msg.StopTransactionResponse
	if req.IDTag != nil {
		resp.IDTagInfo = &v16msg.IDTagInfo{Status: v16msg.IDTagInfoStatusAccepted}
	}
	h.rec.Response(c.ID(), "StopTransaction", resp, nil)
	return resp, nil
}

func (h *handler) OnStatusNotification(_ context.Context, c *csms.Conn, req v16msg.StatusNotificationRequest) (v16msg.StatusNotificationResponse, error) {
	h.rec.Request(c.ID(), "StatusNotification", req)
	if h.failRate > 0 && (h.failRate >= 1 || rand.Float64() < h.failRate) {
		return v16msg.StatusNotificationResponse{}, ocppj.NewCallError(ocppj.ErrorCodeInternalError, "injected", nil)
	}
	resp := v16msg.StatusNotificationResponse{}
	h.rec.Response(c.ID(), "StatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnMeterValues(_ context.Context, c *csms.Conn, req v16msg.MeterValuesRequest) (v16msg.MeterValuesResponse, error) {
	h.rec.Request(c.ID(), "MeterValues", req)
	if err := h.inject(c.ID(), "MeterValues"); err != nil {
		return v16msg.MeterValuesResponse{}, err
	}
	resp := v16msg.MeterValuesResponse{}
	h.rec.Response(c.ID(), "MeterValues", resp, nil)
	return resp, nil
}

func (h *handler) OnDiagnosticsStatusNotification(_ context.Context, c *csms.Conn, req v16msg.DiagnosticsStatusNotificationRequest) (v16msg.DiagnosticsStatusNotificationResponse, error) {
	h.rec.Request(c.ID(), "DiagnosticsStatusNotification", req)
	resp := v16msg.DiagnosticsStatusNotificationResponse{}
	h.rec.Response(c.ID(), "DiagnosticsStatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnFirmwareStatusNotification(_ context.Context, c *csms.Conn, req v16msg.FirmwareStatusNotificationRequest) (v16msg.FirmwareStatusNotificationResponse, error) {
	h.rec.Request(c.ID(), "FirmwareStatusNotification", req)
	resp := v16msg.FirmwareStatusNotificationResponse{}
	h.rec.Response(c.ID(), "FirmwareStatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnDataTransfer(_ context.Context, c *csms.Conn, req v16msg.DataTransferRequest) (v16msg.DataTransferResponse, error) {
	h.rec.Request(c.ID(), "DataTransfer", req)
	var data any
	if req.Data != nil {
		data = *req.Data
	}
	d := common.DecideDataTransfer(req.VendorID, data)
	switch d.Kind {
	case common.DTPanic:
		h.rec.PanicPending(c.ID(), "DataTransfer")
		panic(common.PanicMessage)
	case common.DTError:
		err := errors.New(common.ErrorMessage)
		h.rec.Response(c.ID(), "DataTransfer", nil, err)
		return v16msg.DataTransferResponse{}, err
	}
	var resp v16msg.DataTransferResponse
	switch d.Kind {
	case common.DTReject:
		resp.Status = v16msg.DataTransferResponseStatusRejected
	case common.DTSleep:
		time.Sleep(d.Sleep)
		resp.Status = v16msg.DataTransferResponseStatusAccepted
	default:
		resp.Status = v16msg.DataTransferResponseStatusAccepted
		resp.Data = req.Data
	}
	h.rec.Response(c.ID(), "DataTransfer", resp, nil)
	return resp, nil
}

// ---------------------------------------------------------------- control

type callFn func(ctx context.Context, c *csms.Conn, payload []byte) ([]byte, error)

func typed[Req, Resp any](m ocppj.Message[Req, Resp]) callFn {
	return func(ctx context.Context, c *csms.Conn, payload []byte) ([]byte, error) {
		var req Req
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, &common.KindedError{Kind: common.KindInvalid, Err: err}
		}
		resp, err := csms.Call(ctx, c, m, req)
		if err != nil {
			return nil, err
		}
		return json.Marshal(resp)
	}
}

var calls = map[string]callFn{
	"RemoteStartTransaction": typed(profiles.RemoteStartTransaction),
	"RemoteStopTransaction":  typed(profiles.RemoteStopTransaction),
	"Reset":                  typed(profiles.Reset),
	"ChangeConfiguration":    typed(profiles.ChangeConfiguration),
	"GetConfiguration":       typed(profiles.GetConfiguration),
	"ChangeAvailability":     typed(profiles.ChangeAvailability),
	"UnlockConnector":        typed(profiles.UnlockConnector),
	"ClearCache":             typed(profiles.ClearCache),
	"DataTransfer":           typed(profiles.DataTransfer),
	"TriggerMessage":         typed(profiles.TriggerMessage),
	"ReserveNow":             typed(profiles.ReserveNow),
	"CancelReservation":      typed(profiles.CancelReservation),
	"SetChargingProfile":     typed(profiles.SetChargingProfile),
	"ClearChargingProfile":   typed(profiles.ClearChargingProfile),
	"GetCompositeSchedule":   typed(profiles.GetCompositeSchedule),
	"GetLocalListVersion":    typed(profiles.GetLocalListVersion),
	"SendLocalList":          typed(profiles.SendLocalList),
	"UpdateFirmware":         typed(profiles.UpdateFirmware),
	"GetDiagnostics":         typed(profiles.GetDiagnostics),
}

type backend struct{ srv *csms.Server }

func (b *backend) Lib() string { return "gocpp" }

func (b *backend) Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/shiv3/gocpp" {
				if d.Replace != nil {
					return d.Replace.Path + " (replaced)"
				}
				return d.Version
			}
		}
	}
	return "unknown"
}

func (b *backend) Call(ctx context.Context, cpID, action string, payload json.RawMessage) (json.RawMessage, error) {
	fn, ok := calls[action]
	if !ok {
		return nil, &common.KindedError{Kind: common.KindInvalid, Err: fmt.Errorf("unsupported action %q", action)}
	}
	c, err := b.srv.Lookup(ctx, cpID)
	if err != nil {
		return nil, &common.KindedError{Kind: common.KindNotConnected, Err: errors.New("charge point not connected")}
	}
	out, err := fn(ctx, c, payload)
	if err != nil {
		var ce *ocppj.CallError
		var ke *common.KindedError
		switch {
		case errors.As(err, &ke):
			return nil, err
		case errors.Is(err, ocppj.ErrCallTimeout), errors.Is(err, context.DeadlineExceeded):
			return nil, &common.KindedError{Kind: common.KindTimeout, Err: err}
		case errors.Is(err, ocppj.ErrNotConnected), errors.Is(err, ocppj.ErrConnClosed):
			return nil, &common.KindedError{Kind: common.KindNotConnected, Err: err}
		case errors.As(err, &ce):
			return nil, &common.CallError{Code: ce.WireCode(ocppj.V16), Description: ce.Description}
		case strings.Contains(err.Error(), "jsonschema validation failed"):
			// The request failed gocpp's own outbound schema check.
			return nil, &common.KindedError{Kind: common.KindInvalid, Err: err}
		}
		return nil, err
	}
	return out, nil
}

// Raw is unsupported: gocpp does not expose a raw frame write.
func (b *backend) Raw(string, string) error { return common.ErrUnsupported }

// Disconnect closes one charge point's connection with Server.Disconnect
// (WebSocket close 1000).
func (b *backend) Disconnect(id string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := b.srv.Disconnect(ctx, id); err != nil {
		if errors.Is(err, ocppj.ErrNotConnected) {
			return &common.KindedError{Kind: common.KindNotConnected, Err: err}
		}
		return err
	}
	return nil
}

// Shutdown calls Server.Shutdown. New charge point upgrades are refused with
// HTTP 503 afterwards; the HTTP listener (and this control API) stays up.
func (b *backend) Shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return b.srv.Shutdown(ctx)
}

// ---------------------------------------------------------------- slog

// captureHandler records Warn and Error records as "log" events.
type captureHandler struct {
	rec   *common.Recorder
	attrs []slog.Attr
	group string
}

func (h *captureHandler) Enabled(_ context.Context, l slog.Level) bool { return l >= slog.LevelWarn }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]any{}
	add := func(a slog.Attr) {
		k := a.Key
		if h.group != "" {
			k = h.group + "." + k
		}
		attrs[k] = fmt.Sprint(a.Value.Resolve().Any())
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(func(a slog.Attr) bool { add(a); return true })
	e := common.Event{Kind: common.KindLog, Level: strings.ToLower(r.Level.String()), Msg: r.Message, Attrs: attrs}
	if id, ok := attrs["cp_id"].(string); ok {
		e.CpID = id
	}
	h.rec.Add(e)
	return nil
}

func (h *captureHandler) WithAttrs(a []slog.Attr) slog.Handler {
	n := *h
	n.attrs = append(append([]slog.Attr{}, h.attrs...), a...)
	return &n
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	n := *h
	if n.group != "" {
		name = n.group + "." + name
	}
	n.group = name
	return &n
}
