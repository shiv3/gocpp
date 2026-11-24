// Package controller is the smart-charging controller profile of gocpp-csms
// serve mode: a state model of charge points and connectors, REST endpoints to
// read it, an opt-in setup sequence after boot, a charging-limit helper, a
// remote-start policy, a boot policy, lenient-parsing toggles and fault
// injection for the CSMS's answers.
//
// Everything is opt-in. Without any controller flag the extension registers
// nothing, so the wire behaviour is that of plain serve mode.
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/dispatch"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/station"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
)

// DefaultMeasurands is the list setup tries to enable, one at a time.
const DefaultMeasurands = "Power.Active.Import,Energy.Active.Import.Register,Current.Import,Voltage,Current.Offered,Power.Offered,SoC"

// Options are the controller flags.
type Options struct {
	Enabled bool

	Setup            bool
	SkipAvailability bool
	MeterInterval    time.Duration
	PingInterval     time.Duration
	Measurands       string
	WatchdogN        int
	TriggerBootAfter time.Duration

	ProfileID      int
	NominalVoltage int

	RemoteStartIDTag string

	BootPolicy       bool
	KnownCPs         string
	BootPendingInter int

	LenientEmptyNumbers bool
	LenientRateUnit     bool
	LenientVendorMeas   bool
	LenientTimeLocation string
	DropInCompat        bool

	Faults bool
}

// Ext is the serve-mode extension.
type Ext struct {
	Opts Options

	logger *slog.Logger
	reg    *station.Registry
	faults *Faults
	known  map[string]bool

	mu        sync.Mutex
	rt        *server.Runtime
	startedAt map[string]string // cpID|connector -> status the remote start was sent for
	remoteID  atomic.Int64
	now       func() time.Time
}

// Name implements server.Extension.
func (e *Ext) Name() string { return "controller" }

// Flags implements server.Extension.
func (e *Ext) Flags(b *config.Binder) {
	o := &e.Opts
	b.Bool(&o.Enabled, "controller", false, "enable the controller profile: charge point state model, GET /chargepoints, limit helper")
	b.Bool(&o.Setup, "controller-setup", false, "after an Accepted boot, configure the charge point (availability, configuration read, measurand detection, intervals, triggers)")
	b.Bool(&o.SkipAvailability, "controller-setup-skip-availability", false, "setup: do not send ChangeAvailability(Operative)")
	b.Duration(&o.MeterInterval, "controller-meter-interval", 10*time.Second, "setup: sampled meter value interval to configure")
	b.Duration(&o.PingInterval, "controller-ping-interval", 30*time.Second, "setup: WebSocket ping interval to configure on the charge point (0 = leave unchanged)")
	b.String(&o.Measurands, "controller-measurands", DefaultMeasurands, `setup: measurands to try, comma separated; "-name" removes one from the default list`)
	b.Int(&o.WatchdogN, "controller-watchdog", 0, "meter watchdog: TriggerMessage(MeterValues) when a running transaction sent no meter values for this many meter intervals (0 = off)")
	b.Duration(&o.TriggerBootAfter, "controller-trigger-boot-after", 0, "send TriggerMessage(BootNotification) when a charge point has not booted this long after connecting (0 = off)")
	b.Int(&o.ProfileID, "controller-profile-id", 1, "charging profile id used by the limit helper")
	b.Int(&o.NominalVoltage, "controller-nominal-voltage", 230, "nominal voltage for converting between A and W in the limit helper")
	b.String(&o.RemoteStartIDTag, "controller-remote-start-idtag", "", "remote-start policy: start a transaction with this id tag when a connector is Preparing (1.6) / Occupied (2.x) without a transaction")
	b.Bool(&o.BootPolicy, "controller-boot-policy", false, "answer BootNotification Pending until the charge point is in --controller-known-cps")
	b.String(&o.KnownCPs, "controller-known-cps", "", "boot policy: comma separated charge point ids that are accepted")
	b.Int(&o.BootPendingInter, "controller-boot-pending-interval", 30, "boot policy: interval (seconds) of the Pending answer")
	b.Bool(&o.LenientEmptyNumbers, "lenient-empty-numbers", false, `read "" where a number is expected as 0 (implies --schema lenient)`)
	b.Bool(&o.LenientRateUnit, "lenient-missing-charging-rate-unit", false, "read a charging schedule without chargingRateUnit as W (implies --schema lenient)")
	b.Bool(&o.LenientVendorMeas, "lenient-vendor-measurands", false, "pass vendor-specific measurands (implies --schema lenient)")
	b.String(&o.LenientTimeLocation, "lenient-time-location", "", `time zone (IANA name) for date-times without a zone designator (implies --schema lenient)`)
	b.Bool(&o.DropInCompat, "compat-preset", false, "apply the library's drop-in compatibility preset (csms.WithOCPPGoCompat); later toggles still win")
	b.Bool(&o.Faults, "controller-faults", false, "enable fault injection for CSMS answers, configured at runtime with PUT /faults")
}

func (o *Options) active() bool {
	return o.Enabled || o.Setup || o.WatchdogN > 0 || o.RemoteStartIDTag != "" || o.BootPolicy || o.Faults || o.TriggerBootAfter > 0
}

func (o *Options) lenient() bool {
	return o.LenientEmptyNumbers || o.LenientRateUnit || o.LenientVendorMeas || o.LenientTimeLocation != ""
}

// Setup implements server.Extension.
func (e *Ext) Setup(h *server.Hooks) error {
	o := &e.Opts
	e.logger = h.Logger.With("component", "controller")
	e.now = time.Now
	e.startedAt = map[string]string{}
	if err := e.lenientOptions(h); err != nil {
		return err
	}
	if !o.active() {
		return nil
	}
	if o.NominalVoltage <= 0 {
		return errors.New("--controller-nominal-voltage must be positive")
	}
	if o.WatchdogN > 0 && o.MeterInterval <= 0 {
		return errors.New("--controller-watchdog needs a positive --controller-meter-interval")
	}
	e.reg = station.NewRegistry(h.Txns)
	e.faults = &Faults{}
	e.known = map[string]bool{}
	for _, id := range strings.Split(o.KnownCPs, ",") {
		if id = strings.TrimSpace(id); id != "" {
			e.known[id] = true
		}
	}

	if o.BootPolicy {
		for _, v := range []ocppj.Version{ocppj.V16, ocppj.V201, ocppj.V21} {
			h.Inbound.Override(v, "BootNotification", func(ctx context.Context, call inbound.Call) (json.RawMessage, error) {
				if e.known[call.CPID] {
					return h.Inbound.Default(ctx, call)
				}
				return json.Marshal(map[string]any{
					"currentTime": e.now().UTC().Format("2006-01-02T15:04:05.000Z07:00"),
					"interval":    o.BootPendingInter,
					"status":      "Pending",
				})
			})
		}
	}
	if o.Faults {
		h.Inbound.Use(e.faults.Middleware)
	}
	h.Inbound.Observe(e.observe)

	h.Handle("GET /chargepoints", http.HandlerFunc(e.listStations))
	h.Handle("GET /chargepoints/{id}", http.HandlerFunc(e.getStation))
	h.Handle("POST /chargepoints/{id}/limit", http.HandlerFunc(e.postLimit))
	h.Handle("DELETE /chargepoints/{id}/limit", http.HandlerFunc(e.deleteLimit))
	h.Handle("GET /chargepoints/{id}/schedule", http.HandlerFunc(e.getSchedule))
	if o.Faults {
		h.Handle("GET /faults", http.HandlerFunc(e.faults.get))
		h.Handle("PUT /faults", http.HandlerFunc(e.faults.put))
		h.Handle("DELETE /faults", http.HandlerFunc(e.faults.del))
	}
	if o.TriggerBootAfter > 0 {
		h.OnConnect(e.onConnect)
	}
	h.OnStart(func(ctx context.Context, rt *server.Runtime) error {
		e.mu.Lock()
		e.rt = rt
		e.mu.Unlock()
		if o.WatchdogN > 0 {
			go e.watchdog(ctx)
		}
		return nil
	})
	return nil
}

// lenientOptions maps the lenient toggles to csms options.
func (e *Ext) lenientOptions(h *server.Hooks) error {
	o := &e.Opts
	var opts []csms.Option
	if o.DropInCompat {
		opts = append(opts, csms.WithOCPPGoCompat())
	}
	if o.lenient() {
		opts = append(opts, csms.WithLenientSchema())
		if o.LenientEmptyNumbers {
			opts = append(opts, csms.WithLenientEmptyNumbers())
		}
		if o.LenientRateUnit {
			opts = append(opts, csms.WithLenientMissingChargingRateUnit())
		}
		if o.LenientVendorMeas {
			opts = append(opts, csms.WithLenientVendorMeasurands())
		}
		if o.LenientTimeLocation != "" {
			loc, err := time.LoadLocation(o.LenientTimeLocation)
			if err != nil {
				return fmt.Errorf("--lenient-time-location: %w", err)
			}
			opts = append(opts, csms.WithLenientTimeLocation(loc))
		}
	}
	if len(opts) > 0 {
		h.CSMSOptions(opts...)
	}
	return nil
}

func (e *Ext) runtime() *server.Runtime {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.rt
}

// send validates and sends a command, decoding nothing.
func (e *Ext) send(ctx context.Context, cp, action string, payload any) (json.RawMessage, error) {
	rt := e.runtime()
	if rt == nil {
		return nil, &dispatch.Error{Status: http.StatusServiceUnavailable, Msg: "server is not running"}
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return rt.Dispatcher.Send(ctx, cp, action, b)
}

func (e *Ext) version(cp string) (ocppj.Version, bool) {
	rt := e.runtime()
	if rt == nil {
		return "", false
	}
	c, ok := rt.CSMS.Get(cp)
	if !ok {
		return "", false
	}
	return c.Version(), true
}

// observe feeds the state model and runs the policies.
func (e *Ext) observe(ctx context.Context, call inbound.Call, resp json.RawMessage, err error) {
	ch := e.reg.Observe(call, resp, err)
	o := &e.Opts
	switch {
	case ch.Booted && ch.BootStatus == "Accepted" && o.Setup:
		e.afterReply(ctx, func(ctx context.Context) { e.runSetup(ctx, ch.CPID, ch.Version) })
	case ch.StatusChanged && o.RemoteStartIDTag != "":
		e.maybeRemoteStart(ctx, ch)
	}
}

// afterReply runs fn once the answer reached the charge point; it runs fn in a
// goroutine when there is no reply to wait for (unit tests).
func (e *Ext) afterReply(ctx context.Context, fn func(ctx context.Context)) {
	if csms.AfterReply(ctx, fn) {
		return
	}
	go fn(context.WithoutCancel(ctx))
}

func (e *Ext) onConnect(c *csms.Conn) {
	id := c.ID()
	time.AfterFunc(e.Opts.TriggerBootAfter, func() {
		if st, ok := e.reg.Snapshot(id); ok && st.Boot != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := e.send(ctx, id, "TriggerMessage", map[string]any{"requestedMessage": "BootNotification"}); err != nil {
			e.logger.Warn("trigger BootNotification", "cp_id", id, "error", err)
		}
	})
}

func (e *Ext) maybeRemoteStart(ctx context.Context, ch station.Change) {
	want := "Preparing"
	if ch.Version != ocppj.V16 {
		want = "Occupied"
	}
	if ch.Status != want || ch.ConnectorID == 0 && ch.Version == ocppj.V16 {
		return
	}
	st, ok := e.reg.Snapshot(ch.CPID)
	if !ok {
		return
	}
	for _, c := range st.Connectors {
		if c.ID == ch.ConnectorID && c.TransactionID != "" {
			return
		}
	}
	tag := e.Opts.RemoteStartIDTag
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		var err error
		if ch.Version == ocppj.V16 {
			_, err = e.send(ctx, ch.CPID, "RemoteStartTransaction", map[string]any{"connectorId": ch.ConnectorID, "idTag": tag})
		} else {
			_, err = e.send(ctx, ch.CPID, "RequestStartTransaction", map[string]any{
				"evseId":        ch.ConnectorID,
				"remoteStartId": int(e.remoteID.Add(1)),
				"idToken":       map[string]any{"idToken": tag, "type": "Central"},
			})
		}
		if err != nil {
			e.logger.Warn("remote start", "cp_id", ch.CPID, "connector", ch.ConnectorID, "error", err)
		}
	}()
}

// watchdog asks for meter values of running transactions that went quiet.
func (e *Ext) watchdog(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			e.watchdogTick(ctx, e.now())
		}
	}
}

func (e *Ext) watchdogTick(ctx context.Context, now time.Time) {
	after := time.Duration(e.Opts.WatchdogN) * e.Opts.MeterInterval
	for _, tg := range e.reg.StaleMeters(now, after) {
		payload := map[string]any{"requestedMessage": "MeterValues"}
		if tg.Version == ocppj.V16 {
			payload["connectorId"] = tg.ConnectorID
		} else {
			payload["evse"] = map[string]any{"id": tg.ConnectorID}
		}
		go func() {
			cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			if _, err := e.send(cctx, tg.CPID, "TriggerMessage", payload); err != nil {
				e.logger.Warn("meter watchdog trigger", "cp_id", tg.CPID, "connector", tg.ConnectorID, "error", err)
			}
		}()
	}
}

// --- REST: state ---

func (e *Ext) views() []station.Station {
	list := e.reg.List()
	rt := e.runtime()
	if rt == nil {
		return list
	}
	conn := map[string]bool{}
	for _, id := range rt.CSMS.Connections() {
		conn[id] = true
	}
	seen := map[string]bool{}
	for i := range list {
		list[i].Connected = conn[list[i].ID]
		seen[list[i].ID] = true
	}
	for id := range conn {
		if !seen[id] {
			s := station.Station{ID: id, Connected: true, Connectors: []station.Connector{}}
			if c, ok := rt.CSMS.Get(id); ok {
				s.Version = string(c.Version())
			}
			list = append(list, s)
		}
	}
	sortStations(list)
	return list
}

func (e *Ext) listStations(w http.ResponseWriter, _ *http.Request) {
	httpapi.WriteResult(w, e.views())
}

func (e *Ext) getStation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	for _, s := range e.views() {
		if s.ID == id {
			httpapi.WriteResult(w, s)
			return
		}
	}
	httpapi.WriteError(w, http.StatusNotFound, fmt.Sprintf("unknown charge point %q", id))
}
