// Command cs-ocppgo is the ocpp-go half of the parity test CSMS pair.
package main

import (
	_ "net/http/pprof"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"regexp"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/localauth"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/reservation"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"

	"github.com/shiv3/gocpp/interop/parity/common"
)

func main() {
	listen := flag.String("listen", "", "listen address, e.g. 127.0.0.1:9000 (required; only the port is used, ocpp-go binds all interfaces)")
	eventsPath := flag.String("events", "", "append events as JSONL to this file")
	callTimeout := flag.Duration("call-timeout", 10*time.Second, "timeout for CSMS-initiated calls")
	lite := flag.Bool("lite", false, "load-test mode: keep no event history, only counters (see /control/count)")
	pprofAddr := flag.String("pprof", "", "serve net/http/pprof on this address, e.g. 127.0.0.1:6060")
	flag.Parse()
	if *pprofAddr != "" {
		go func() { log.Println(http.ListenAndServe(*pprofAddr, nil)) }()
	}
	if *listen == "" {
		log.Fatal("-listen is required")
	}
	_, portStr, err := net.SplitHostPort(*listen)
	if err != nil {
		log.Fatal(err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatal(err)
	}

	rec, err := common.NewRecorder(*eventsPath)
	if err != nil {
		log.Fatal(err)
	}
	rec.SetLite(*lite)
	lg := &captureLogger{rec: rec}
	ws.SetLogger(lg)
	ocppj.SetLogger(lg)

	// Library defaults everywhere except the dispatcher timeout, which is set
	// to -call-timeout (the default is 30s) so a call that gets no answer is
	// cancelled and unblocks the per-CP request queue.
	dispatcher := ocppj.NewDefaultServerDispatcher(ocppj.NewFIFOQueueMap(0))
	dispatcher.SetTimeout(*callTimeout)
	wsServer := ws.NewServer()
	endpoint := ocppj.NewServer(wsServer, dispatcher, nil,
		core.Profile, localauth.Profile, firmware.Profile, reservation.Profile,
		remotetrigger.Profile, smartcharging.Profile)
	cs := ocpp16.NewCentralSystem(endpoint, wsServer)

	h := &handler{rec: rec}
	cs.SetCoreHandler(h)
	cs.SetFirmwareManagementHandler(h)
	cs.SetLocalAuthListHandler(emptyHandler{})
	cs.SetReservationHandler(emptyHandler{})
	cs.SetRemoteTriggerHandler(emptyHandler{})
	cs.SetSmartChargingHandler(emptyHandler{})

	be := &backend{cs: cs, ws: wsServer, rec: rec, lg: lg}
	cs.SetNewChargingStationValidationHandler(func(id string, r *http.Request) bool {
		// Called before the upgrade: an id that is still registered is about to
		// be replaced (ocpp-go is last-wins).
		if rec.IsConnected(id) {
			lg.markReplacing(id)
		}
		return true
	})
	cs.SetNewChargePointHandler(func(cp ocpp16.ChargePointConnection) {
		rec.Connect(cp.ID(), "")
	})
	cs.SetChargePointDisconnectedHandler(func(cp ocpp16.ChargePointConnection) {
		cause, raw := be.cause(cp.ID())
		rec.Disconnect(cp.ID(), cause, raw)
	})

	// ocpp-go also reports central system errors through this channel. The
	// ws.Server errors channel is not drained: the same errors arrive through
	// the ws logger, so reading both would record each of them twice.
	drain := func(ch <-chan error) {
		go func() {
			for err := range ch {
				rec.Error("", err.Error())
			}
		}()
	}
	drain(cs.Errors())

	ctl := common.Control(rec, be, *callTimeout)
	wsServer.AddHttpHandler("/control/{rest:.*}", ctl.ServeHTTP)

	fmt.Fprintf(os.Stderr, "cs-ocppgo listening on :%d (requested %s)\n", port, *listen)
	cs.Start(port, "/ocpp/{ws-id}")
}

// ---------------------------------------------------------------- handlers

type emptyHandler struct{}

type handler struct {
	rec *common.Recorder
	tx  common.TxCounter
}

func (h *handler) OnBootNotification(id string, req *core.BootNotificationRequest) (*core.BootNotificationConfirmation, error) {
	h.rec.Request(id, "BootNotification", req)
	resp := core.NewBootNotificationConfirmation(types.NewDateTime(common.FixedTimeT), common.BootInterval, core.RegistrationStatusAccepted)
	h.rec.Response(id, "BootNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnHeartbeat(id string, req *core.HeartbeatRequest) (*core.HeartbeatConfirmation, error) {
	h.rec.Request(id, "Heartbeat", req)
	resp := core.NewHeartbeatConfirmation(types.NewDateTime(common.FixedTimeT))
	h.rec.Response(id, "Heartbeat", resp, nil)
	return resp, nil
}

func idTagInfo(idTag string) *types.IdTagInfo {
	return types.NewIdTagInfo(types.AuthorizationStatus(common.AuthStatus(idTag)))
}

func (h *handler) OnAuthorize(id string, req *core.AuthorizeRequest) (*core.AuthorizeConfirmation, error) {
	h.rec.Request(id, "Authorize", req)
	resp := core.NewAuthorizationConfirmation(idTagInfo(req.IdTag))
	h.rec.Response(id, "Authorize", resp, nil)
	return resp, nil
}

func (h *handler) OnStartTransaction(id string, req *core.StartTransactionRequest) (*core.StartTransactionConfirmation, error) {
	h.rec.Request(id, "StartTransaction", req)
	resp := core.NewStartTransactionConfirmation(idTagInfo(req.IdTag), int(h.tx.Next()))
	h.rec.Response(id, "StartTransaction", resp, nil)
	return resp, nil
}

func (h *handler) OnStopTransaction(id string, req *core.StopTransactionRequest) (*core.StopTransactionConfirmation, error) {
	h.rec.Request(id, "StopTransaction", req)
	resp := core.NewStopTransactionConfirmation()
	if req.IdTag != "" {
		resp.IdTagInfo = types.NewIdTagInfo(types.AuthorizationStatusAccepted)
	}
	h.rec.Response(id, "StopTransaction", resp, nil)
	return resp, nil
}

func (h *handler) OnStatusNotification(id string, req *core.StatusNotificationRequest) (*core.StatusNotificationConfirmation, error) {
	h.rec.Request(id, "StatusNotification", req)
	resp := core.NewStatusNotificationConfirmation()
	h.rec.Response(id, "StatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnMeterValues(id string, req *core.MeterValuesRequest) (*core.MeterValuesConfirmation, error) {
	h.rec.Request(id, "MeterValues", req)
	resp := core.NewMeterValuesConfirmation()
	h.rec.Response(id, "MeterValues", resp, nil)
	return resp, nil
}

func (h *handler) OnDiagnosticsStatusNotification(id string, req *firmware.DiagnosticsStatusNotificationRequest) (*firmware.DiagnosticsStatusNotificationConfirmation, error) {
	h.rec.Request(id, "DiagnosticsStatusNotification", req)
	resp := firmware.NewDiagnosticsStatusNotificationConfirmation()
	h.rec.Response(id, "DiagnosticsStatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnFirmwareStatusNotification(id string, req *firmware.FirmwareStatusNotificationRequest) (*firmware.FirmwareStatusNotificationConfirmation, error) {
	h.rec.Request(id, "FirmwareStatusNotification", req)
	resp := firmware.NewFirmwareStatusNotificationConfirmation()
	h.rec.Response(id, "FirmwareStatusNotification", resp, nil)
	return resp, nil
}

func (h *handler) OnDataTransfer(id string, req *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	h.rec.Request(id, "DataTransfer", req)
	d := common.DecideDataTransfer(req.VendorId, req.Data)
	switch d.Kind {
	case common.DTPanic:
		h.rec.PanicPending(id, "DataTransfer")
		panic(common.PanicMessage)
	case common.DTError:
		err := errors.New(common.ErrorMessage)
		h.rec.Response(id, "DataTransfer", nil, err)
		return nil, err
	}
	resp := core.NewDataTransferConfirmation(core.DataTransferStatusAccepted)
	switch d.Kind {
	case common.DTReject:
		resp.Status = core.DataTransferStatusRejected
	case common.DTSleep:
		time.Sleep(d.Sleep)
	default:
		resp.Data = req.Data
	}
	h.rec.Response(id, "DataTransfer", resp, nil)
	return resp, nil
}

// ---------------------------------------------------------------- control

var requests = map[string]func() ocpp.Request{
	"RemoteStartTransaction": func() ocpp.Request { return &core.RemoteStartTransactionRequest{} },
	"RemoteStopTransaction":  func() ocpp.Request { return &core.RemoteStopTransactionRequest{} },
	"Reset":                  func() ocpp.Request { return &core.ResetRequest{} },
	"ChangeConfiguration":    func() ocpp.Request { return &core.ChangeConfigurationRequest{} },
	"GetConfiguration":       func() ocpp.Request { return &core.GetConfigurationRequest{} },
	"ChangeAvailability":     func() ocpp.Request { return &core.ChangeAvailabilityRequest{} },
	"UnlockConnector":        func() ocpp.Request { return &core.UnlockConnectorRequest{} },
	"ClearCache":             func() ocpp.Request { return &core.ClearCacheRequest{} },
	"DataTransfer":           func() ocpp.Request { return &core.DataTransferRequest{} },
	"TriggerMessage":         func() ocpp.Request { return &remotetrigger.TriggerMessageRequest{} },
	"ReserveNow":             func() ocpp.Request { return &reservation.ReserveNowRequest{} },
	"CancelReservation":      func() ocpp.Request { return &reservation.CancelReservationRequest{} },
	"SetChargingProfile":     func() ocpp.Request { return &smartcharging.SetChargingProfileRequest{} },
	"ClearChargingProfile":   func() ocpp.Request { return &smartcharging.ClearChargingProfileRequest{} },
	"GetCompositeSchedule":   func() ocpp.Request { return &smartcharging.GetCompositeScheduleRequest{} },
	"GetLocalListVersion":    func() ocpp.Request { return &localauth.GetLocalListVersionRequest{} },
	"SendLocalList":          func() ocpp.Request { return &localauth.SendLocalListRequest{} },
	"UpdateFirmware":         func() ocpp.Request { return &firmware.UpdateFirmwareRequest{} },
	"GetDiagnostics":         func() ocpp.Request { return &firmware.GetDiagnosticsRequest{} },
}

type backend struct {
	cs  ocpp16.CentralSystem
	ws  *ws.Server
	rec *common.Recorder
	lg  *captureLogger

	mu         sync.Mutex
	serverStop map[string]bool // ids whose close we initiated
}

func (b *backend) Lib() string { return "ocpp-go" }

func (b *backend) Version() string {
	if bi, ok := debug.ReadBuildInfo(); ok {
		for _, d := range bi.Deps {
			if d.Path == "github.com/lorenzodonini/ocpp-go" {
				if d.Replace != nil {
					return d.Replace.Path + "@" + d.Replace.Version
				}
				return d.Version
			}
		}
	}
	return "unknown"
}

func (b *backend) Call(ctx context.Context, cpID, action string, payload json.RawMessage) (json.RawMessage, error) {
	mk, ok := requests[action]
	if !ok {
		return nil, &common.KindedError{Kind: common.KindInvalid, Err: fmt.Errorf("unsupported action %q", action)}
	}
	req := mk()
	if err := json.Unmarshal(payload, req); err != nil {
		return nil, &common.KindedError{Kind: common.KindInvalid, Err: err}
	}
	type result struct {
		resp ocpp.Response
		err  error
	}
	done := make(chan result, 1)
	err := b.cs.SendRequestAsync(cpID, req, func(resp ocpp.Response, err error) {
		done <- result{resp, err}
	})
	if err != nil {
		kind := common.KindOther
		switch {
		case strings.Contains(fmt.Sprintf("%T", err), "ValidationErrors"):
			kind = common.KindInvalid
		case strings.Contains(err.Error(), "not connected"), strings.Contains(err.Error(), "no client"):
			kind = common.KindNotConnected
		}
		return nil, &common.KindedError{Kind: kind, Err: err}
	}
	select {
	case <-ctx.Done():
		return nil, &common.KindedError{Kind: common.KindTimeout, Err: ctx.Err()}
	case r := <-done:
		if r.err != nil {
			var oe *ocpp.Error
			if errors.As(r.err, &oe) {
				if oe.Code == ocppj.GenericError && oe.Description == "Request timed out" {
					return nil, &common.KindedError{Kind: common.KindTimeout, Err: errors.New(oe.Description)}
				}
				return nil, &common.CallError{Code: string(oe.Code), Description: oe.Description}
			}
			return nil, r.err
		}
		return json.Marshal(r.resp)
	}
}

// Raw is unsupported: ocpp-go's public API only writes typed, validated
// messages (ws.Server.Write is on the unexported-by-contract ocppj path and
// would bypass request/response bookkeeping), so no raw frame injection.
func (b *backend) Raw(string, string) error { return common.ErrUnsupported }

func (b *backend) stop(id string) error {
	b.mu.Lock()
	if b.serverStop == nil {
		b.serverStop = map[string]bool{}
	}
	b.serverStop[id] = true
	b.mu.Unlock()
	err := b.ws.StopConnection(id, websocket.CloseError{Code: websocket.CloseNormalClosure})
	if err != nil {
		b.mu.Lock()
		delete(b.serverStop, id)
		b.mu.Unlock()
	}
	return err
}

// Disconnect uses ws.Server.StopConnection with a normal closure (1000).
func (b *backend) Disconnect(id string) error { return b.stop(id) }

// Shutdown calls StopConnection for every connected id. ws.Server.Stop would
// also close the HTTP listener (and with it this control API), so it is not
// used.
func (b *backend) Shutdown() error {
	var errs []string
	for _, id := range b.rec.Connections() {
		if err := b.stop(id); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

// cause decides the normalized disconnect cause. ocpp-go does not hand the
// application a cause, so it is inferred from what we initiated and from the
// library's own debug log of the read error.
func (b *backend) cause(id string) (cause, raw string) {
	b.mu.Lock()
	stopped := b.serverStop[id]
	delete(b.serverStop, id)
	b.mu.Unlock()
	readErr := b.lg.takeReadError(id)
	switch {
	case stopped:
		return common.CauseServerClosed, "StopConnection"
	case b.lg.takeReplacing(id):
		return common.CauseReplaced, "client already exists, closing stale connection"
	case readErr != "":
		return common.ClassifyReadError(readErr), readErr
	}
	return common.CauseUnknown, ""
}

// ---------------------------------------------------------------- logging

var readErrRe = regexp.MustCompile(`^handling read error for (.+?): (.*)$`)

// captureLogger implements ocpp-go's logging.Logger. Error-level lines become
// "error" events; one Debug line is parsed to learn why a read loop ended.
type captureLogger struct {
	rec       *common.Recorder
	mu        sync.Mutex
	readErrs  map[string]string
	replacing map[string]bool
}

func (l *captureLogger) Debug(args ...interface{}) {}
func (l *captureLogger) Info(args ...interface{})  {}
func (l *captureLogger) Infof(format string, args ...interface{}) {
}
func (l *captureLogger) Debugf(format string, args ...interface{}) {
	if !strings.HasPrefix(format, "handling read error") {
		return
	}
	m := readErrRe.FindStringSubmatch(fmt.Sprintf(format, args...))
	if m == nil {
		return
	}
	l.mu.Lock()
	if l.readErrs == nil {
		l.readErrs = map[string]string{}
	}
	l.readErrs[m[1]] = m[2]
	l.mu.Unlock()
}
func (l *captureLogger) Error(args ...interface{}) {
	l.rec.Add(common.Event{Kind: common.KindError, Level: "error", Detail: strings.TrimSpace(fmt.Sprint(args...)), Msg: "library log"})
}
func (l *captureLogger) Errorf(format string, args ...interface{}) {
	l.rec.Add(common.Event{Kind: common.KindError, Level: "error", Detail: fmt.Sprintf(format, args...), Msg: "library log"})
}

func (l *captureLogger) takeReadError(id string) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.readErrs[id]
	delete(l.readErrs, id)
	return v
}

func (l *captureLogger) markReplacing(id string) {
	l.mu.Lock()
	if l.replacing == nil {
		l.replacing = map[string]bool{}
	}
	l.replacing[id] = true
	l.mu.Unlock()
}

func (l *captureLogger) takeReplacing(id string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	v := l.replacing[id]
	delete(l.replacing, id)
	return v
}
