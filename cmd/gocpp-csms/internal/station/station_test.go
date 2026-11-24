package station_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/station"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/txn"
	"github.com/shiv3/gocpp/core/ocppj"
)

func observe(r *station.Registry, v ocppj.Version, action, payload, resp string) station.Change {
	return r.Observe(inbound.Call{CPID: "CP", Version: v, Action: action, Payload: json.RawMessage(payload)}, json.RawMessage(resp), nil)
}

func connector(t *testing.T, r *station.Registry) station.Connector {
	t.Helper()
	s, ok := r.Snapshot("CP")
	if !ok {
		t.Fatal("unknown station")
	}
	for _, c := range s.Connectors {
		if c.ID == 1 {
			return c
		}
	}
	t.Fatal("no connector 1")
	return station.Connector{}
}

func TestStatusOrdering(t *testing.T) {
	r := station.NewRegistry(nil)
	ch := observe(r, ocppj.V16, "StatusNotification", `{"connectorId":1,"status":"Charging","errorCode":"NoError","timestamp":"2026-01-01T10:00:10Z"}`, `{}`)
	if !ch.StatusChanged {
		t.Error("first status not a change")
	}
	// An older report arriving late is ignored.
	ch = observe(r, ocppj.V16, "StatusNotification", `{"connectorId":1,"status":"Preparing","errorCode":"NoError","timestamp":"2026-01-01T10:00:05Z"}`, `{}`)
	if ch.StatusChanged || connector(t, r).Status != "Charging" {
		t.Errorf("stale status applied: %+v", connector(t, r))
	}
	ch = observe(r, ocppj.V16, "StatusNotification", `{"connectorId":1,"status":"Faulted","errorCode":"GroundFailure","timestamp":"2026-01-01T10:00:20Z"}`, `{}`)
	if c := connector(t, r); !ch.StatusChanged || c.Status != "Faulted" || c.ErrorCode != "GroundFailure" {
		t.Errorf("connector = %+v", c)
	}
}

func TestMeasurementsScaledAndOrdered(t *testing.T) {
	r := station.NewRegistry(nil)
	observe(r, ocppj.V16, "MeterValues", `{"connectorId":1,"meterValue":[
		{"timestamp":"2026-01-01T10:00:10Z","sampledValue":[
			{"value":"2.5","measurand":"Energy.Active.Import.Register","unit":"kWh"},
			{"value":"7.2","measurand":"Power.Active.Import","unit":"kW"},
			{"value":"16500","measurand":"Current.Import","phase":"L1","unit":"mA"},
			{"value":"230.1","measurand":"Voltage","phase":"L1-N"}]},
		{"timestamp":"2026-01-01T10:00:00Z","sampledValue":[{"value":"1","measurand":"Power.Active.Import","unit":"kW"}]}]}`, `{}`)
	got := map[string]station.Measurement{}
	for _, m := range connector(t, r).Measurements {
		got[m.Measurand+"/"+m.Phase] = m
	}
	for key, want := range map[string]struct {
		v    float64
		unit string
	}{
		"Energy.Active.Import.Register/": {2500, "Wh"},
		"Power.Active.Import/":           {7200, "W"}, // the older sample of the same message is dropped
		"Current.Import/L1":              {16.5, "A"},
		"Voltage/L1-N":                   {230.1, "V"},
	} {
		if m := got[key]; m.Value != want.v || m.Unit != want.unit {
			t.Errorf("%s = %v %s, want %v %s", key, m.Value, m.Unit, want.v, want.unit)
		}
	}
}

func TestTransactionRecoveryAndReboot(t *testing.T) {
	txns := txn.New()
	r := station.NewRegistry(txns)
	observe(r, ocppj.V16, "BootNotification", `{"chargePointVendor":"V","chargePointModel":"M"}`, `{"status":"Accepted"}`)
	st := txns.Start16("CP", 1, "TAG", 0, time.Now())
	observe(r, ocppj.V16, "StatusNotification", `{"connectorId":1,"status":"Charging","errorCode":"NoError","timestamp":"2026-01-01T10:00:00Z"}`, `{}`)
	// Unknown transaction recovered from the meter values.
	observe(r, ocppj.V16, "MeterValues", `{"connectorId":1,"transactionId":`+st.ID+`,"meterValue":[{"sampledValue":[{"value":"1"}]}]}`, `{}`)
	if got := connector(t, r).TransactionID; got != st.ID {
		t.Fatalf("recovered transaction = %q", got)
	}
	// A new boot ends the transaction and lets a fresh status clock start.
	ch := observe(r, ocppj.V16, "BootNotification", `{"chargePointVendor":"V","chargePointModel":"M"}`, `{"status":"Accepted"}`)
	if !ch.Booted || ch.BootStatus != "Accepted" {
		t.Errorf("change = %+v", ch)
	}
	if connector(t, r).TransactionID != "" {
		t.Error("transaction survived the reboot")
	}
	if len(txns.List(txn.Filter{CPID: "CP", ActiveOnly: true})) != 0 {
		t.Error("store still has an active transaction")
	}
	observe(r, ocppj.V16, "StatusNotification", `{"connectorId":1,"status":"Available","errorCode":"NoError","timestamp":"2025-12-31T00:00:00Z"}`, `{}`)
	if connector(t, r).Status != "Available" {
		t.Error("status after reboot was treated as stale")
	}
}

func TestStopAndAvailableClearTransaction(t *testing.T) {
	r := station.NewRegistry(nil)
	observe(r, ocppj.V16, "StartTransaction", `{"connectorId":1,"idTag":"T","meterStart":0}`, `{"transactionId":5,"idTagInfo":{"status":"Accepted"}}`)
	if connector(t, r).TransactionID != "5" {
		t.Fatal("transaction not tracked")
	}
	observe(r, ocppj.V16, "StopTransaction", `{"transactionId":5,"meterStop":10}`, `{}`)
	if connector(t, r).TransactionID != "" {
		t.Error("StopTransaction did not clear the transaction")
	}
}

func TestStaleMeters(t *testing.T) {
	r := station.NewRegistry(nil)
	t0 := time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	now := t0
	r.SetClock(func() time.Time { return now })
	observe(r, ocppj.V16, "StartTransaction", `{"connectorId":1,"idTag":"T","meterStart":0}`, `{"transactionId":5}`)
	now = t0.Add(20 * time.Second)
	if got := r.StaleMeters(now, 30*time.Second); len(got) != 0 {
		t.Errorf("early: %v", got)
	}
	now = t0.Add(40 * time.Second)
	if got := r.StaleMeters(now, 30*time.Second); len(got) != 1 || got[0].ConnectorID != 1 {
		t.Errorf("stale: %v", got)
	}
	// Asked again at once: not before another interval.
	if got := r.StaleMeters(now.Add(time.Second), 30*time.Second); len(got) != 0 {
		t.Errorf("repeat: %v", got)
	}
}

func TestTransactionEvent2x(t *testing.T) {
	r := station.NewRegistry(nil)
	observe(r, ocppj.V201, "TransactionEvent", `{"eventType":"Started","timestamp":"2026-01-01T10:00:00Z","transactionInfo":{"transactionId":"a"},"evse":{"id":1}}`, `{}`)
	if connector(t, r).TransactionID != "a" {
		t.Fatal("transaction not tracked")
	}
	observe(r, ocppj.V201, "TransactionEvent", `{"eventType":"Ended","timestamp":"2026-01-01T10:10:00Z","transactionInfo":{"transactionId":"a"},"evse":{"id":1}}`, `{}`)
	if connector(t, r).TransactionID != "" {
		t.Error("Ended did not clear the transaction")
	}
}
