// Package station is the state model of the controller profile of
// gocpp-csms: what the CSMS knows about each charge point and connector,
// built from the CALLs the charge point sends (boot data, connector status
// and error code, meter values, transactions).
//
// The model works for OCPP 1.6 and for 2.0.1/2.1. Connectors are keyed by the
// connector id in 1.6 and by the EVSE id in 2.x; id 0 is the charge point as a
// whole (1.6 only).
package station

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/txn"
	"github.com/shiv3/gocpp/core/ocppj"
)

// Boot is the identity a charge point reported in its last BootNotification.
type Boot struct {
	Vendor          string    `json:"vendor,omitempty"`
	Model           string    `json:"model,omitempty"`
	SerialNumber    string    `json:"serialNumber,omitempty"`
	FirmwareVersion string    `json:"firmwareVersion,omitempty"`
	Reason          string    `json:"reason,omitempty"`
	Status          string    `json:"status,omitempty"`
	At              time.Time `json:"at"`
}

// Measurement is the last value reported for one measurand and phase, scaled
// to the base unit (Wh, W, A, V, ...: no k or m prefix).
type Measurement struct {
	Measurand string    `json:"measurand"`
	Phase     string    `json:"phase,omitempty"`
	Value     float64   `json:"value"`
	Unit      string    `json:"unit"`
	Context   string    `json:"context,omitempty"`
	Time      time.Time `json:"time"`
}

// Connector is the state of one connector (1.6) or EVSE (2.x).
type Connector struct {
	ID         int       `json:"id"`
	Status     string    `json:"status,omitempty"`
	ErrorCode  string    `json:"errorCode,omitempty"`
	StatusTime time.Time `json:"statusTime,omitempty"`
	// TransactionID is the running transaction ("" = none).
	TransactionID string `json:"transactionId,omitempty"`
	// LastMeterAt is when the last meter values (or the transaction start,
	// if later) arrived; the meter watchdog works from it.
	LastMeterAt  time.Time     `json:"lastMeterAt,omitempty"`
	Measurements []Measurement `json:"measurements,omitempty"`
}

// Limit is a charging limit the CSMS applied through the limit helper.
type Limit struct {
	At            time.Time `json:"at"`
	ConnectorID   int       `json:"connectorId"`
	Purpose       string    `json:"purpose"`
	Unit          string    `json:"unit"`
	Value         float64   `json:"value"`
	Phases        int       `json:"phases,omitempty"`
	StackLevel    int       `json:"stackLevel"`
	ProfileID     int       `json:"profileId"`
	TransactionID string    `json:"transactionId,omitempty"`
}

// Capabilities are the smart-charging properties learned during setup.
type Capabilities struct {
	// RateUnit is the charging rate unit to use: "A" (default) or "W".
	RateUnit string `json:"rateUnit"`
	// MaxStackLevel is the highest stack level; -1 when unknown.
	MaxStackLevel int `json:"maxStackLevel"`
	// PhaseSwitching reports support for switching between 1 and 3 phases.
	PhaseSwitching bool `json:"phaseSwitching"`
	// RemoteTrigger is false only when the charge point announced its
	// feature profiles and RemoteTrigger is not among them.
	RemoteTrigger bool `json:"remoteTrigger"`
	// Measurands are the sampled measurands the charge point accepted.
	Measurands []string `json:"measurands,omitempty"`
}

// Step is one entry of the setup report.
type Step struct {
	At     time.Time `json:"at"`
	Action string    `json:"action"`
	Detail string    `json:"detail,omitempty"`
	Result string    `json:"result"`
}

// Station is the state of one charge point.
type Station struct {
	ID           string            `json:"cpId"`
	Version      string            `json:"version"`
	Connected    bool              `json:"connected"`
	Boot         *Boot             `json:"boot,omitempty"`
	LastSeen     time.Time         `json:"lastSeen"`
	Config       map[string]string `json:"config,omitempty"`
	Capabilities Capabilities      `json:"capabilities"`
	Connectors   []Connector       `json:"connectors"`
	Limits       []Limit           `json:"limits,omitempty"`
	Setup        []Step            `json:"setup,omitempty"`
	connectors   map[int]*Connector
	measure      map[int]map[mkey]*Measurement
}

type mkey struct{ measurand, phase string }

// Registry holds the state of every charge point seen.
type Registry struct {
	mu       sync.Mutex
	stations map[string]*Station
	txns     *txn.Store
	now      func() time.Time
}

// NewRegistry returns an empty registry. txns, if set, has a charge point's
// transactions ended when it reboots.
func NewRegistry(txns *txn.Store) *Registry {
	return &Registry{stations: map[string]*Station{}, txns: txns, now: time.Now}
}

// SetClock replaces the clock (tests).
func (r *Registry) SetClock(now func() time.Time) { r.now = now }

func (r *Registry) get(id string, v ocppj.Version) *Station {
	s, ok := r.stations[id]
	if !ok {
		s = &Station{ID: id, Config: map[string]string{}, connectors: map[int]*Connector{}, measure: map[int]map[mkey]*Measurement{},
			Capabilities: Capabilities{RateUnit: "A", MaxStackLevel: -1, RemoteTrigger: true}}
		r.stations[id] = s
	}
	if v != "" {
		s.Version = string(v)
	}
	return s
}

func (s *Station) connector(id int) *Connector {
	c, ok := s.connectors[id]
	if !ok {
		c = &Connector{ID: id}
		s.connectors[id] = c
	}
	return c
}

// Change describes what an observed CALL changed, for policies layered on the
// model (remote start, setup).
type Change struct {
	CPID    string
	Version ocppj.Version
	// Booted is set for a BootNotification; BootStatus is the answer.
	Booted     bool
	BootStatus string
	// StatusChanged is set when a StatusNotification changed a connector's
	// status; ConnectorID is then that connector.
	StatusChanged bool
	ConnectorID   int
	Status        string
}

// Observe feeds one handled inbound CALL into the model and returns what it
// changed. It is meant for inbound.Handler.Observe.
func (r *Registry) Observe(call inbound.Call, resp json.RawMessage, callErr error) Change {
	ch := Change{CPID: call.CPID, Version: call.Version}
	r.mu.Lock()
	defer r.mu.Unlock()
	s := r.get(call.CPID, call.Version)
	now := r.now()
	s.LastSeen = now
	if callErr != nil {
		return ch
	}
	is16 := call.Version == ocppj.V16
	switch call.Action {
	case "BootNotification":
		ch.Booted = true
		ch.BootStatus = r.boot(s, call, resp, now)
	case "StatusNotification":
		if is16 {
			ch.ConnectorID, ch.Status, ch.StatusChanged = r.status16(s, call.Payload, now)
		} else {
			ch.ConnectorID, ch.Status, ch.StatusChanged = r.status2(s, call.Payload, now)
		}
	case "MeterValues":
		if is16 {
			r.meterValues16(s, call.Payload, now)
		} else {
			r.meterValues2(s, call.Payload, now)
		}
	case "StartTransaction":
		r.start16(s, call.Payload, resp, now)
	case "StopTransaction":
		r.stop16(s, call.Payload)
	case "TransactionEvent":
		r.transactionEvent(s, call.Payload, now)
	case "NotifyEvent":
		r.notifyEvent(s, call.Payload, now)
	}
	return ch
}

func (r *Registry) boot(s *Station, call inbound.Call, resp json.RawMessage, now time.Time) string {
	b := &Boot{At: now}
	if call.Version == ocppj.V16 {
		var p struct {
			V string `json:"chargePointVendor"`
			M string `json:"chargePointModel"`
			S string `json:"chargePointSerialNumber"`
			F string `json:"firmwareVersion"`
		}
		_ = json.Unmarshal(call.Payload, &p)
		b.Vendor, b.Model, b.SerialNumber, b.FirmwareVersion = p.V, p.M, p.S, p.F
	} else {
		var p struct {
			Reason          string `json:"reason"`
			ChargingStation struct {
				Model           string `json:"model"`
				VendorName      string `json:"vendorName"`
				SerialNumber    string `json:"serialNumber"`
				FirmwareVersion string `json:"firmwareVersion"`
			} `json:"chargingStation"`
		}
		_ = json.Unmarshal(call.Payload, &p)
		b.Vendor, b.Model, b.SerialNumber, b.FirmwareVersion = p.ChargingStation.VendorName, p.ChargingStation.Model, p.ChargingStation.SerialNumber, p.ChargingStation.FirmwareVersion
		b.Reason = p.Reason
	}
	var ans struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(resp, &ans)
	b.Status = ans.Status
	s.Boot = b

	// A boot is a reboot: transactions did not survive it, and status
	// timestamps restart from the charge point's new clock.
	for _, c := range s.connectors {
		c.TransactionID = ""
		c.StatusTime = time.Time{}
		c.LastMeterAt = time.Time{}
	}
	if r.txns != nil {
		r.txns.EndAll(s.ID, "Reboot")
	}
	return b.Status
}

func (r *Registry) status16(s *Station, payload json.RawMessage, now time.Time) (int, string, bool) {
	var p struct {
		ConnectorID int       `json:"connectorId"`
		Status      string    `json:"status"`
		ErrorCode   string    `json:"errorCode"`
		Timestamp   time.Time `json:"timestamp"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return 0, "", false
	}
	return p.ConnectorID, p.Status, r.applyStatus(s, p.ConnectorID, p.Status, p.ErrorCode, p.Timestamp, now)
}

func (r *Registry) status2(s *Station, payload json.RawMessage, now time.Time) (int, string, bool) {
	var p struct {
		Timestamp       time.Time `json:"timestamp"`
		ConnectorStatus string    `json:"connectorStatus"`
		EVSEID          int       `json:"evseId"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return 0, "", false
	}
	return p.EVSEID, p.ConnectorStatus, r.applyStatus(s, p.EVSEID, p.ConnectorStatus, "", p.Timestamp, now)
}

// applyStatus stores a connector status unless it is older than the stored
// one. It reports whether the status value changed.
func (r *Registry) applyStatus(s *Station, id int, status, errorCode string, ts, now time.Time) bool {
	if ts.IsZero() {
		ts = now
	}
	c := s.connector(id)
	if !c.StatusTime.IsZero() && ts.Before(c.StatusTime) {
		return false
	}
	changed := c.Status != status
	c.Status, c.ErrorCode, c.StatusTime = status, errorCode, ts
	if errorCode == "NoError" {
		c.ErrorCode = ""
	}
	// An available connector has no transaction: drop a stale one.
	if status == "Available" {
		c.TransactionID = ""
	}
	return changed
}

func (r *Registry) start16(s *Station, payload, resp json.RawMessage, now time.Time) {
	var p struct {
		ConnectorID int `json:"connectorId"`
	}
	var a struct {
		TransactionID int64 `json:"transactionId"`
	}
	if json.Unmarshal(payload, &p) != nil || json.Unmarshal(resp, &a) != nil || a.TransactionID == 0 {
		return
	}
	c := s.connector(p.ConnectorID)
	c.TransactionID = strconv.FormatInt(a.TransactionID, 10)
	c.LastMeterAt = now
}

func (r *Registry) stop16(s *Station, payload json.RawMessage) {
	var p struct {
		TransactionID int64 `json:"transactionId"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	id := strconv.FormatInt(p.TransactionID, 10)
	for _, c := range s.connectors {
		if c.TransactionID == id {
			c.TransactionID = ""
		}
	}
}

type sampledValue struct {
	Value     flex   `json:"value"`
	Context   string `json:"context"`
	Measurand string `json:"measurand"`
	Phase     string `json:"phase"`
	Unit      string `json:"unit"`
	UoM       *struct {
		Unit       string `json:"unit"`
		Multiplier int    `json:"multiplier"`
	} `json:"unitOfMeasure"`
}

type meterValue struct {
	Timestamp    time.Time      `json:"timestamp"`
	SampledValue []sampledValue `json:"sampledValue"`
}

func (r *Registry) meterValues16(s *Station, payload json.RawMessage, now time.Time) {
	var p struct {
		ConnectorID   int          `json:"connectorId"`
		TransactionID *int64       `json:"transactionId"`
		MeterValue    []meterValue `json:"meterValue"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	c := s.connector(p.ConnectorID)
	if p.TransactionID != nil && *p.TransactionID > 0 && c.TransactionID == "" && c.Status != "Available" {
		c.TransactionID = strconv.FormatInt(*p.TransactionID, 10)
	}
	r.storeMeter(s, p.ConnectorID, p.MeterValue, now)
}

func (r *Registry) meterValues2(s *Station, payload json.RawMessage, now time.Time) {
	var p struct {
		EVSEID     int          `json:"evseId"`
		MeterValue []meterValue `json:"meterValue"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	r.storeMeter(s, p.EVSEID, p.MeterValue, now)
}

func (r *Registry) transactionEvent(s *Station, payload json.RawMessage, now time.Time) {
	var p struct {
		EventType       string `json:"eventType"`
		TransactionInfo struct {
			TransactionID string `json:"transactionId"`
		} `json:"transactionInfo"`
		EVSE *struct {
			ID int `json:"id"`
		} `json:"evse"`
		MeterValue []meterValue `json:"meterValue"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	id := 0
	if p.EVSE != nil {
		id = p.EVSE.ID
	} else {
		for _, c := range s.connectors {
			if c.TransactionID == p.TransactionInfo.TransactionID {
				id = c.ID
			}
		}
	}
	if id == 0 && p.EVSE == nil {
		return
	}
	c := s.connector(id)
	if p.EventType == "Ended" {
		c.TransactionID = ""
	} else {
		if c.TransactionID != p.TransactionInfo.TransactionID {
			c.LastMeterAt = now
		}
		c.TransactionID = p.TransactionInfo.TransactionID
	}
	r.storeMeter(s, id, p.MeterValue, now)
}

// notifyEvent reads a connector availability change reported as an event.
func (r *Registry) notifyEvent(s *Station, payload json.RawMessage, now time.Time) {
	var p struct {
		EventData []struct {
			Timestamp   time.Time `json:"timestamp"`
			ActualValue string    `json:"actualValue"`
			Component   struct {
				Name string `json:"name"`
				EVSE *struct {
					ID int `json:"id"`
				} `json:"evse"`
			} `json:"component"`
			Variable struct {
				Name string `json:"name"`
			} `json:"variable"`
		} `json:"eventData"`
	}
	if json.Unmarshal(payload, &p) != nil {
		return
	}
	for _, e := range p.EventData {
		if e.Component.Name == "Connector" && e.Variable.Name == "AvailabilityState" && e.Component.EVSE != nil {
			r.applyStatus(s, e.Component.EVSE.ID, e.ActualValue, "", e.Timestamp, now)
		}
	}
}

// storeMeter keeps the newest value of every measurand and phase; values with
// an older timestamp than the stored one are dropped.
func (r *Registry) storeMeter(s *Station, id int, mvs []meterValue, now time.Time) {
	if len(mvs) == 0 {
		return
	}
	c := s.connector(id)
	c.LastMeterAt = now
	m := s.measure[id]
	if m == nil {
		m = map[mkey]*Measurement{}
		s.measure[id] = m
	}
	sorted := append([]meterValue(nil), mvs...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp.Before(sorted[j].Timestamp) })
	for _, mv := range sorted {
		ts := mv.Timestamp
		if ts.IsZero() {
			ts = now
		}
		for _, sv := range mv.SampledValue {
			meas := sv.Measurand
			if meas == "" {
				meas = "Energy.Active.Import.Register"
			}
			unit, mult := sv.Unit, 0
			if sv.UoM != nil {
				unit, mult = sv.UoM.Unit, sv.UoM.Multiplier
			}
			val, unit := Scale(float64(sv.Value)*math.Pow10(mult), unit, meas)
			k := mkey{meas, sv.Phase}
			if old, ok := m[k]; ok && ts.Before(old.Time) {
				continue
			}
			m[k] = &Measurement{Measurand: meas, Phase: sv.Phase, Value: val, Unit: unit, Context: sv.Context, Time: ts}
		}
	}
	c.Measurements = c.Measurements[:0]
	for _, v := range m {
		c.Measurements = append(c.Measurements, *v)
	}
	sort.Slice(c.Measurements, func(i, j int) bool {
		a, b := c.Measurements[i], c.Measurements[j]
		if a.Measurand != b.Measurand {
			return a.Measurand < b.Measurand
		}
		return a.Phase < b.Phase
	})
}

// Scale converts value in unit to the base unit: a k or m prefix is applied
// (kWh -> Wh, kW -> W, mA -> A). An empty unit takes the default of the
// measurand (Wh for energy registers, W, A, V, ...).
func Scale(value float64, unit, measurand string) (float64, string) {
	if unit == "" {
		return value, defaultUnit(measurand)
	}
	if len(unit) > 1 {
		switch unit[0] {
		case 'k':
			if baseUnits[unit[1:]] {
				return value * 1000, unit[1:]
			}
		case 'm':
			if baseUnits[unit[1:]] {
				return value / 1000, unit[1:]
			}
		}
	}
	return value, unit
}

var baseUnits = map[string]bool{"Wh": true, "W": true, "VA": true, "var": true, "varh": true, "VAh": true, "A": true, "V": true}

func defaultUnit(measurand string) string {
	switch {
	case strings.HasPrefix(measurand, "Energy.Active"), strings.HasPrefix(measurand, "Energy.Reactive"):
		return "Wh"
	case strings.HasPrefix(measurand, "Power.Active"), strings.HasPrefix(measurand, "Power.Offered"):
		return "W"
	case strings.HasPrefix(measurand, "Current"):
		return "A"
	case strings.HasPrefix(measurand, "Voltage"):
		return "V"
	case measurand == "Frequency":
		return "Hz"
	case measurand == "SoC":
		return "Percent"
	case measurand == "Temperature":
		return "Celsius"
	}
	return ""
}

// flex decodes a JSON number or numeric string; anything else is 0.
type flex float64

func (n *flex) UnmarshalJSON(b []byte) error {
	var f float64
	if json.Unmarshal(b, &f) == nil {
		*n = flex(f)
		return nil
	}
	var s string
	if json.Unmarshal(b, &s) == nil {
		if f, err := strconv.ParseFloat(s, 64); err == nil {
			*n = flex(f)
		}
	}
	return nil
}

// Mutate runs fn on a station's mutable settings under the registry lock.
func (r *Registry) Mutate(id string, fn func(*Station)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn(r.get(id, ""))
}

// AddStep appends a setup report entry.
func (r *Registry) AddStep(id, action, detail, result string) {
	r.Mutate(id, func(s *Station) {
		s.Setup = append(s.Setup, Step{At: r.now(), Action: action, Detail: detail, Result: result})
	})
}

// ResetSetup clears the setup report (a new setup run starts).
func (r *Registry) ResetSetup(id string) {
	r.Mutate(id, func(s *Station) { s.Setup = nil })
}

// SetLimit records an applied limit, replacing one with the same connector
// and purpose.
func (r *Registry) SetLimit(id string, l Limit) {
	r.Mutate(id, func(s *Station) {
		l.At = r.now()
		for i, o := range s.Limits {
			if o.ConnectorID == l.ConnectorID && o.Purpose == l.Purpose {
				s.Limits[i] = l
				return
			}
		}
		s.Limits = append(s.Limits, l)
	})
}

// ClearLimits forgets the limits of a connector (and purpose, if not empty);
// connector < 0 matches every connector.
func (r *Registry) ClearLimits(id string, connector int, purpose string) {
	r.Mutate(id, func(s *Station) {
		kept := s.Limits[:0]
		for _, l := range s.Limits {
			if (connector < 0 || l.ConnectorID == connector) && (purpose == "" || l.Purpose == purpose) {
				continue
			}
			kept = append(kept, l)
		}
		s.Limits = kept
	})
}

// Snapshot returns a copy of one station.
func (r *Registry) Snapshot(id string) (Station, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.stations[id]
	if !ok {
		return Station{}, false
	}
	return r.copyLocked(s), true
}

// List returns a copy of every station ordered by id.
func (r *Registry) List() []Station {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Station, 0, len(r.stations))
	for _, s := range r.stations {
		out = append(out, r.copyLocked(s))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) copyLocked(s *Station) Station {
	c := *s
	if s.Boot != nil {
		b := *s.Boot
		c.Boot = &b
	}
	c.Config = make(map[string]string, len(s.Config))
	for k, v := range s.Config {
		c.Config[k] = v
	}
	c.Capabilities.Measurands = append([]string(nil), s.Capabilities.Measurands...)
	c.Limits = append([]Limit(nil), s.Limits...)
	c.Setup = append([]Step(nil), s.Setup...)
	c.Connectors = make([]Connector, 0, len(s.connectors))
	for _, k := range s.connectors {
		cc := *k
		cc.Measurements = append([]Measurement(nil), k.Measurements...)
		c.Connectors = append(c.Connectors, cc)
	}
	sort.Slice(c.Connectors, func(i, j int) bool { return c.Connectors[i].ID < c.Connectors[j].ID })
	return c
}

// StaleMeters returns the connectors that run a transaction and have sent no
// meter values for longer than after, and marks them as checked at now so
// they are returned again only after another interval.
func (r *Registry) StaleMeters(now time.Time, after time.Duration) []Target {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []Target
	for _, s := range r.stations {
		for _, c := range s.connectors {
			if c.TransactionID == "" || c.LastMeterAt.IsZero() || now.Sub(c.LastMeterAt) < after {
				continue
			}
			c.LastMeterAt = now
			out = append(out, Target{CPID: s.ID, Version: ocppj.Version(s.Version), ConnectorID: c.ID})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CPID != out[j].CPID {
			return out[i].CPID < out[j].CPID
		}
		return out[i].ConnectorID < out[j].ConnectorID
	})
	return out
}

// Target names a connector of a charge point.
type Target struct {
	CPID        string
	Version     ocppj.Version
	ConnectorID int
}
