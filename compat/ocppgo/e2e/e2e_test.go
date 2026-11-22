// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

// Package e2e runs ocpp-go's unchanged charge point client against the
// central system of this module over real WebSocket connections, written the
// way an application written against ocpp-go is: with the default runtime
// (gocpp) and the module's own ws.Server.
package e2e

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
	"github.com/sirupsen/logrus"
)

// ---- central system side: the usage pattern of the production consumer ----

type csHandler struct {
	boots, heartbeats, meters, statuses atomic.Int64
	txID                                atomic.Int64
}

func (h *csHandler) OnAuthorize(id string, r *core.AuthorizeRequest) (*core.AuthorizeConfirmation, error) {
	return core.NewAuthorizationConfirmation(types.NewIdTagInfo(types.AuthorizationStatusAccepted)), nil
}
func (h *csHandler) OnBootNotification(id string, r *core.BootNotificationRequest) (*core.BootNotificationConfirmation, error) {
	h.boots.Add(1)
	return core.NewBootNotificationConfirmation(types.NewDateTime(time.Now()), 60, core.RegistrationStatusAccepted), nil
}
func (h *csHandler) OnDataTransfer(id string, r *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	switch r.VendorId {
	case "panic":
		panic("application handler bug")
	case "error":
		return nil, errors.New("handler failed")
	}
	return core.NewDataTransferConfirmation(core.DataTransferStatusAccepted), nil
}
func (h *csHandler) OnHeartbeat(id string, r *core.HeartbeatRequest) (*core.HeartbeatConfirmation, error) {
	h.heartbeats.Add(1)
	return core.NewHeartbeatConfirmation(types.NewDateTime(time.Now())), nil
}
func (h *csHandler) OnMeterValues(id string, r *core.MeterValuesRequest) (*core.MeterValuesConfirmation, error) {
	h.meters.Add(1)
	return core.NewMeterValuesConfirmation(), nil
}
func (h *csHandler) OnStatusNotification(id string, r *core.StatusNotificationRequest) (*core.StatusNotificationConfirmation, error) {
	h.statuses.Add(1)
	return core.NewStatusNotificationConfirmation(), nil
}
func (h *csHandler) OnStartTransaction(id string, r *core.StartTransactionRequest) (*core.StartTransactionConfirmation, error) {
	return core.NewStartTransactionConfirmation(types.NewIdTagInfo(types.AuthorizationStatusAccepted), int(h.txID.Add(1))), nil
}
func (h *csHandler) OnStopTransaction(id string, r *core.StopTransactionRequest) (*core.StopTransactionConfirmation, error) {
	return core.NewStopTransactionConfirmation(), nil
}
func (h *csHandler) OnDiagnosticsStatusNotification(id string, r *firmware.DiagnosticsStatusNotificationRequest) (*firmware.DiagnosticsStatusNotificationConfirmation, error) {
	return firmware.NewDiagnosticsStatusNotificationConfirmation(), nil
}
func (h *csHandler) OnFirmwareStatusNotification(id string, r *firmware.FirmwareStatusNotificationRequest) (*firmware.FirmwareStatusNotificationConfirmation, error) {
	return firmware.NewFirmwareStatusNotificationConfirmation(), nil
}

var setLoggers sync.Once

type central struct {
	cs       ocpp16.CentralSystem
	srv      *ws.Server
	h        *csHandler
	port     int
	connects atomic.Int64
	drops    atomic.Int64
}

// freePort avoids ws.Server.Addr(), which ocpp-go reads without synchronisation.
func freePort(t testing.TB) int {
	l, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func startCentral(t testing.TB) *central {
	// ocpp-go's loggers are package globals: set them once.
	setLoggers.Do(func() {
		logger := logrus.New()
		logger.SetLevel(logrus.ErrorLevel)
		ocppj.SetLogger(logger)
		ws.SetLogger(logger)
	})

	c := &central{h: &csHandler{}, port: freePort(t)}
	c.srv = ws.NewServer()
	c.cs = ocpp16.NewCentralSystem(nil, c.srv) // nil endpoint: the default runtime, i.e. gocpp
	c.cs.SetCoreHandler(c.h)
	c.cs.SetFirmwareManagementHandler(c.h)
	c.cs.SetNewChargePointHandler(func(cp ocpp16.ChargePointConnection) { c.connects.Add(1) })
	c.cs.SetChargePointDisconnectedHandler(func(cp ocpp16.ChargePointConnection) { c.drops.Add(1) })
	c.srv.AddHttpHandler("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	tc := ws.NewServerTimeoutConfig()
	tc.PingWait = 30 * time.Second
	c.srv.SetTimeoutConfig(tc)
	c.srv.SetCheckOriginHandler(func(r *http.Request) bool { return true })
	go func() {
		for err := range c.srv.Errors() {
			_ = err // drained, as the consumer does
		}
	}()
	go c.cs.Start(c.port, "/{ws}")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		if resp, err := http.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", c.port)); err == nil {
			resp.Body.Close()
			break
		}
	}
	t.Cleanup(c.srv.Stop)
	return c
}

// ---- charge point side: ocpp-go's unchanged client ----

type cpHandler struct {
	block     chan struct{} // when non-nil, RemoteStartTransaction waits on it
	remoteIDs chan string
}

func (h *cpHandler) OnChangeAvailability(r *core.ChangeAvailabilityRequest) (*core.ChangeAvailabilityConfirmation, error) {
	return core.NewChangeAvailabilityConfirmation(core.AvailabilityStatusAccepted), nil
}
func (h *cpHandler) OnChangeConfiguration(r *core.ChangeConfigurationRequest) (*core.ChangeConfigurationConfirmation, error) {
	return core.NewChangeConfigurationConfirmation(core.ConfigurationStatusAccepted), nil
}
func (h *cpHandler) OnClearCache(r *core.ClearCacheRequest) (*core.ClearCacheConfirmation, error) {
	return core.NewClearCacheConfirmation(core.ClearCacheStatusAccepted), nil
}
func (h *cpHandler) OnDataTransfer(r *core.DataTransferRequest) (*core.DataTransferConfirmation, error) {
	return core.NewDataTransferConfirmation(core.DataTransferStatusAccepted), nil
}
func (h *cpHandler) OnGetConfiguration(r *core.GetConfigurationRequest) (*core.GetConfigurationConfirmation, error) {
	return core.NewGetConfigurationConfirmation(nil), nil
}
func (h *cpHandler) OnRemoteStartTransaction(r *core.RemoteStartTransactionRequest) (*core.RemoteStartTransactionConfirmation, error) {
	if h.block != nil {
		<-h.block
	}
	if h.remoteIDs != nil {
		h.remoteIDs <- r.IdTag
	}
	return core.NewRemoteStartTransactionConfirmation(types.RemoteStartStopStatusAccepted), nil
}
func (h *cpHandler) OnRemoteStopTransaction(r *core.RemoteStopTransactionRequest) (*core.RemoteStopTransactionConfirmation, error) {
	return core.NewRemoteStopTransactionConfirmation(types.RemoteStartStopStatusAccepted), nil
}
func (h *cpHandler) OnReset(r *core.ResetRequest) (*core.ResetConfirmation, error) {
	return core.NewResetConfirmation(core.ResetStatusAccepted), nil
}
func (h *cpHandler) OnUnlockConnector(r *core.UnlockConnectorRequest) (*core.UnlockConnectorConfirmation, error) {
	return core.NewUnlockConnectorConfirmation(core.UnlockStatusUnlocked), nil
}
func (h *cpHandler) OnTriggerMessage(r *remotetrigger.TriggerMessageRequest) (*remotetrigger.TriggerMessageConfirmation, error) {
	return remotetrigger.NewTriggerMessageConfirmation(remotetrigger.TriggerMessageStatusAccepted), nil
}

// stopOnce makes Stop idempotent: ocpp-go's client panics when stopped twice.
type stopOnce struct {
	ocpp16.ChargePoint
	once sync.Once
}

func (s *stopOnce) Stop() { s.once.Do(s.ChargePoint.Stop) }

// startMu serializes client starts: ocpp-go's ChargePoint.Start writes a package
// variable (ocppj.FormationViolation), a data race when several start at once.
var startMu sync.Mutex

func connectCP(t testing.TB, c *central, id string, h *cpHandler) ocpp16.ChargePoint {
	cp := &stopOnce{ChargePoint: ocpp16.NewChargePoint(id, nil, nil)}
	cp.SetCoreHandler(h)
	cp.SetRemoteTriggerHandler(h)
	startMu.Lock()
	err := cp.Start(fmt.Sprintf("ws://127.0.0.1:%d", c.port))
	startMu.Unlock()
	if err != nil {
		t.Fatalf("cp %s start: %v", id, err)
	}
	t.Cleanup(cp.Stop)
	return cp
}

func waitFor(t testing.TB, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestSession runs a typical charger session in both directions.
func TestSession(t *testing.T) {
	c := startCentral(t)
	cp := connectCP(t, c, "CP-1", &cpHandler{})
	waitFor(t, "connect callback", func() bool { return c.connects.Load() == 1 })

	boot, err := cp.BootNotification("model", "vendor")
	if err != nil || boot.Status != core.RegistrationStatusAccepted {
		t.Fatalf("boot: %+v %v", boot, err)
	}
	if _, err = cp.Heartbeat(); err != nil {
		t.Fatal(err)
	}
	if _, err = cp.StatusNotification(1, core.NoError, core.ChargePointStatusAvailable); err != nil {
		t.Fatal(err)
	}
	start, err := cp.StartTransaction(1, "TAG", 0, types.NewDateTime(time.Now()))
	if err != nil || start.TransactionId != 1 {
		t.Fatalf("start: %+v %v", start, err)
	}
	mv := []types.MeterValue{{Timestamp: types.NewDateTime(time.Now()), SampledValue: []types.SampledValue{{
		Value: "12.5", Measurand: types.MeasurandEnergyActiveImportRegister, Unit: types.UnitOfMeasureKWh,
		Context: types.ReadingContextSamplePeriodic, Format: types.ValueFormatRaw, Location: types.LocationOutlet}}}}
	if _, err = cp.MeterValues(1, mv, func(r *core.MeterValuesRequest) { r.TransactionId = &start.TransactionId }); err != nil {
		t.Fatal(err)
	}
	if _, err = cp.StopTransaction(13, types.NewDateTime(time.Now()), start.TransactionId, func(r *core.StopTransactionRequest) {
		r.Reason = core.ReasonLocal
	}); err != nil {
		t.Fatal(err)
	}

	// Central system -> charge point, as the consumer does it: SendRequestAsync with a callback.
	type result struct {
		conf ocpp.Response
		err  error
	}
	results := make(chan result, 3)
	cb := func(conf ocpp.Response, err error) { results <- result{conf, err} }
	for _, req := range []ocpp.Request{
		core.NewRemoteStartTransactionRequest("TAG"),
		core.NewChangeConfigurationRequest("MeterValueSampleInterval", "30"),
		remotetrigger.NewTriggerMessageRequest(remotetrigger.MessageTrigger(core.StatusNotificationFeatureName)),
	} {
		if err := c.cs.SendRequestAsync("CP-1", req, cb); err != nil {
			t.Fatalf("send %s: %v", req.GetFeatureName(), err)
		}
	}
	want := []string{core.RemoteStartTransactionFeatureName, core.ChangeConfigurationFeatureName, remotetrigger.TriggerMessageFeatureName}
	for i := 0; i < 3; i++ {
		select {
		case r := <-results:
			if r.err != nil {
				t.Fatalf("response %d: %v", i, r.err)
			}
			if r.conf.GetFeatureName() != want[i] {
				t.Fatalf("response %d is %s, want %s (FIFO order per charge point)", i, r.conf.GetFeatureName(), want[i])
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("no response %d", i)
		}
	}

	// An invalid request is rejected by ocpp-go's own validation, with ocpp-go's error.
	err = c.cs.SendRequestAsync("CP-1", core.NewRemoteStartTransactionRequest(""), cb)
	if err == nil {
		t.Fatal("empty idTag must fail validation")
	}
	// Unknown client.
	if err = c.cs.SendRequestAsync("nobody", core.NewRemoteStartTransactionRequest("TAG"), cb); err == nil {
		t.Fatal("request to an unknown client must fail")
	}

	cp.Stop()
	waitFor(t, "disconnect callback", func() bool { return c.drops.Load() == 1 })
}

// TestHandlerPanicIsContained: with ocpp-go a panic in an application handler
// kills the process. On the gocpp runtime the charger gets InternalError and the
// server keeps serving it and everyone else.
func TestHandlerPanicIsContained(t *testing.T) {
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("the legacy runtime keeps ocpp-go's behaviour: the process exits")
	}
	c := startCentral(t)
	cp := connectCP(t, c, "CP-P", &cpHandler{})
	other := connectCP(t, c, "CP-Q", &cpHandler{})

	_, err := cp.DataTransfer("panic")
	var ocppErr *ocpp.Error
	if !errors.As(err, &ocppErr) || ocppErr.Code != ocppj.InternalError {
		t.Fatalf("want InternalError CALLERROR, got %v", err)
	}
	// A handler that returns an error is answered as ocpp-go does.
	_, err = cp.DataTransfer("error")
	if !errors.As(err, &ocppErr) || ocppErr.Code != ocppj.InternalError || ocppErr.Description != "handler failed" {
		t.Fatalf("want InternalError 'handler failed', got %v", err)
	}
	if _, err = cp.Heartbeat(); err != nil {
		t.Fatalf("same connection after panic: %v", err)
	}
	if _, err = other.Heartbeat(); err != nil {
		t.Fatalf("other connection after panic: %v", err)
	}
}

// TestIsolation: one charger that does not answer a request must not delay
// requests to the other chargers.
func TestIsolation(t *testing.T) {
	c := startCentral(t)
	const n = 200
	stuckH := &cpHandler{block: make(chan struct{})}
	stuck := connectCP(t, c, "CP-STUCK", stuckH)
	cps := make([]ocpp16.ChargePoint, n)
	var wg sync.WaitGroup
	for i := range cps {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			cps[i] = connectCP(t, c, fmt.Sprintf("CP-%03d", i), &cpHandler{})
		}(i)
	}
	wg.Wait()
	waitFor(t, "all connected", func() bool { return c.connects.Load() == n+1 })

	stuckDone := make(chan error, 1)
	if err := c.cs.SendRequestAsync("CP-STUCK", core.NewRemoteStartTransactionRequest("TAG"), func(_ ocpp.Response, err error) {
		stuckDone <- err
	}); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	var done atomic.Int64
	for i := 0; i < n; i++ {
		for k := 0; k < 5; k++ {
			if err := c.cs.SendRequestAsync(fmt.Sprintf("CP-%03d", i), core.NewRemoteStartTransactionRequest("TAG"), func(_ ocpp.Response, err error) {
				if err == nil {
					done.Add(1)
				}
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	waitFor(t, "1000 calls to the healthy chargers", func() bool { return done.Load() == n*5 })
	t.Logf("%d calls to %d healthy chargers answered in %v while one charger held its request", n*5, n, time.Since(start))

	// (The stuck charger is ocpp-go's unchanged client, which runs its handler on
	// the read loop, so it cannot be asked to send anything while it is stuck.)
	_ = stuck
	select {
	case err := <-stuckDone:
		t.Fatalf("stuck request completed early: %v", err)
	default:
	}
	close(stuckH.block)
	select {
	case err := <-stuckDone:
		if err != nil {
			t.Fatalf("stuck request: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stuck request never completed")
	}
}

// TestDisconnectCancelsPending: a request pending when the charger drops is
// reported to the callback instead of being left behind.
func TestDisconnectCancelsPending(t *testing.T) {
	c := startCentral(t)
	h := &cpHandler{block: make(chan struct{})}
	cp := connectCP(t, c, "CP-D", h)
	waitFor(t, "connect callback", func() bool { return c.connects.Load() == 1 })
	got := make(chan error, 1)
	if err := c.cs.SendRequestAsync("CP-D", core.NewRemoteStartTransactionRequest("TAG"), func(_ ocpp.Response, err error) { got <- err }); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	cp.Stop()
	close(h.block)
	select {
	case err := <-got:
		if err == nil {
			t.Fatal("want an error for the request that was pending at disconnect")
		}
		t.Logf("pending request cancelled with: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("callback never invoked")
	}
}

// TestStopConnectionCloseCode: ws.Server.StopConnection with a gorilla CloseError
// reaches the charger as exactly that close frame, and the application is told.
func TestStopConnectionCloseCode(t *testing.T) {
	c := startCentral(t)
	closed := make(chan *websocket.CloseError, 1)
	u := fmt.Sprintf("ws://127.0.0.1:%d/CP-C", c.port)
	conn, _, err := websocket.DefaultDialer.Dial(u, http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	go func() {
		_, _, err := conn.ReadMessage()
		var ce *websocket.CloseError
		if errors.As(err, &ce) {
			closed <- ce
		}
		close(closed)
	}()
	waitFor(t, "connect", func() bool { return c.connects.Load() == 1 })
	if err := c.srv.StopConnection("CP-C", websocket.CloseError{Code: websocket.CloseGoingAway, Text: "maintenance"}); err != nil {
		t.Fatal(err)
	}
	select {
	case ce := <-closed:
		if ce == nil || ce.Code != websocket.CloseGoingAway || ce.Text != "maintenance" {
			t.Fatalf("close frame: %+v", ce)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no close frame")
	}
	waitFor(t, "disconnect callback", func() bool { return c.drops.Load() == 1 })
	if err := c.srv.StopConnection("CP-C", websocket.CloseError{Code: websocket.CloseNormalClosure}); err == nil {
		t.Fatal("stopping a gone connection must fail")
	}
}

// TestServerStopClosesNormally: Stop closes every connection with 1000.
func TestServerStopClosesNormally(t *testing.T) {
	c := startCentral(t)
	u := fmt.Sprintf("ws://127.0.0.1:%d/CP-S", c.port)
	conn, _, err := websocket.DefaultDialer.Dial(u, http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitFor(t, "connect", func() bool { return c.connects.Load() == 1 })
	c.srv.Stop()
	_, _, err = conn.ReadMessage()
	var ce *websocket.CloseError
	if !errors.As(err, &ce) || ce.Code != websocket.CloseNormalClosure {
		t.Fatalf("want a normal closure, got %v", err)
	}
}
