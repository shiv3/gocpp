package inbound

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/actions"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/txn"
	"github.com/shiv3/gocpp/core/ocppj"
)

// samples are request payloads for the actions whose default answer depends
// on the request; every other action is answered from "{}".
var samples = map[string]string{
	"1.6|StartTransaction":          `{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`,
	"1.6|StopTransaction":           `{"transactionId":1,"idTag":"T","meterStop":10,"timestamp":"2026-01-01T00:01:00Z"}`,
	"2.0.1|TransactionEvent":        `{"eventType":"Started","timestamp":"2026-01-01T00:00:00Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"t1"},"idToken":{"idToken":"T","type":"ISO14443"}}`,
	"2.1|TransactionEvent":          `{"eventType":"Started","timestamp":"2026-01-01T00:00:00Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"t1"},"idToken":{"idToken":"T","type":"ISO14443"}}`,
	"2.1|VatNumberValidation":       `{"vatNumber":"NL123","evseId":1}`,
	"2.1|GetCertificateChainStatus": `{"certificateStatusRequests":[{"source":"OCSP","urls":["http://ocsp.example"],"certificateHashData":{"hashAlgorithm":"SHA256","issuerNameHash":"a","issuerKeyHash":"b","serialNumber":"c"}}]}`,
}

// TestEveryActionHasASchemaValidDefault answers every charge-point-initiated
// CALL of every version and validates the answer against its schema.
func TestEveryActionHasASchemaValidDefault(t *testing.T) {
	reg, err := actions.Schemas()
	if err != nil {
		t.Fatal(err)
	}
	h := New(Options{Txns: txn.New()})
	for _, v := range actions.Versions {
		set := actions.SentByCP(v)
		if len(set.Calls) < 14 {
			t.Fatalf("%s: only %d charge point actions found: %v", v, len(set.Calls), set.Calls)
		}
		for _, action := range set.Calls {
			payload := samples[string(v)+"|"+action]
			if payload == "" {
				payload = `{}`
			}
			resp, err := h.Handle(context.Background(), Call{CPID: "CP1", Version: v, Action: action, Payload: json.RawMessage(payload)})
			if err != nil {
				t.Fatalf("%s %s: %v", v, action, err)
			}
			val, ok := reg.Lookup(string(v), action, "response")
			if !ok {
				t.Fatalf("%s %s: no response schema", v, action)
			}
			if err := val.Validate(resp); err != nil {
				t.Errorf("%s %s: default answer %s is not schema-valid: %v", v, action, resp, err)
			}
		}
	}
	if s := actions.SentByCP(ocppj.V21); !s.HasSend("NotifyPeriodicEventStream") {
		t.Fatalf("2.1 SENDs = %v", s.Sends)
	}
}

func TestUnknownActionIsNotSupported(t *testing.T) {
	h := New(Options{Txns: txn.New()})
	for _, c := range []Call{
		{Version: ocppj.V16, Action: "RemoteStartTransaction", Payload: json.RawMessage(`{}`)},
		{Version: ocppj.V201, Action: "StartTransaction", Payload: json.RawMessage(`{}`)},
		{Version: ocppj.V21, Action: "Bogus", Payload: json.RawMessage(`{}`), Send: true},
	} {
		_, err := h.Handle(context.Background(), c)
		var ce *ocppj.CallError
		if !errors.As(err, &ce) || ce.Code != ocppj.ErrorCodeNotSupported {
			t.Fatalf("%s %s: err %v, want NotSupported", c.Version, c.Action, err)
		}
	}
}

func TestTransactionSemantics16(t *testing.T) {
	store := txn.New()
	h := New(Options{Txns: store})
	call := func(action, payload string) string {
		t.Helper()
		resp, err := h.Handle(context.Background(), Call{CPID: "CP1", Version: ocppj.V16, Action: action, Payload: json.RawMessage(payload)})
		if err != nil {
			t.Fatal(err)
		}
		return string(resp)
	}
	if got := call("StartTransaction", `{"connectorId":1,"idTag":"A","meterStart":100,"timestamp":"2026-01-01T00:00:00Z"}`); got != `{"idTagInfo":{"status":"Accepted"},"transactionId":1}` {
		t.Fatalf("first start: %s", got)
	}
	if got := call("StartTransaction", `{"connectorId":2,"idTag":"B","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`); got != `{"idTagInfo":{"status":"Accepted"},"transactionId":2}` {
		t.Fatalf("second start: %s", got)
	}
	if got := call("StopTransaction", `{"transactionId":1,"meterStop":600,"timestamp":"2026-01-01T00:10:00Z"}`); got != `{}` {
		t.Fatalf("stop without idTag: %s", got)
	}
	if got := call("StopTransaction", `{"transactionId":2,"idTag":"B","meterStop":5,"timestamp":"2026-01-01T00:10:00Z"}`); got != `{"idTagInfo":{"status":"Accepted"}}` {
		t.Fatalf("stop with idTag: %s", got)
	}
	tx, ok := store.Get("CP1", "1")
	if !ok || tx.Active || *tx.MeterStart != 100 || *tx.MeterStop != 600 || tx.ConnectorID != 1 || tx.IDTag != "A" {
		t.Fatalf("transaction 1 = %+v", tx)
	}
	if active := store.List(txn.Filter{ActiveOnly: true}); len(active) != 0 {
		t.Fatalf("active = %+v", active)
	}
}

func TestTransactionSemantics2x(t *testing.T) {
	store := txn.New()
	h := New(Options{Txns: store})
	call := func(payload string) string {
		t.Helper()
		resp, err := h.Handle(context.Background(), Call{CPID: "CP1", Version: ocppj.V201, Action: "TransactionEvent", Payload: json.RawMessage(payload)})
		if err != nil {
			t.Fatal(err)
		}
		return string(resp)
	}
	if got := call(`{"eventType":"Started","timestamp":"2026-01-01T00:00:00Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"tx-9","chargingState":"EVConnected"},"idToken":{"idToken":"A","type":"ISO14443"},"evse":{"id":1,"connectorId":1},"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":1.5,"unitOfMeasure":{"unit":"kWh"}}]}]}`); got != `{"idTokenInfo":{"status":"Accepted"}}` {
		t.Fatalf("started: %s", got)
	}
	if got := call(`{"eventType":"Updated","timestamp":"2026-01-01T00:01:00Z","triggerReason":"ChargingStateChanged","seqNo":1,"transactionInfo":{"transactionId":"tx-9","chargingState":"Charging"}}`); got != `{}` {
		t.Fatalf("updated: %s", got)
	}
	tx, _ := store.Get("CP1", "tx-9")
	if !tx.Active || tx.ChargingState != "Charging" || tx.EVSEID != 1 || *tx.MeterStart != 1500 || *tx.LastSeqNo != 1 {
		t.Fatalf("after update: %+v", tx)
	}
	call(`{"eventType":"Ended","timestamp":"2026-01-01T00:02:00Z","triggerReason":"StopAuthorized","seqNo":2,"transactionInfo":{"transactionId":"tx-9","stoppedReason":"Local"},"meterValue":[{"timestamp":"2026-01-01T00:02:00Z","sampledValue":[{"value":2500,"measurand":"Energy.Active.Import.Register"}]}]}`)
	tx, _ = store.Get("CP1", "tx-9")
	if tx.Active || tx.StoppedReason != "Local" || *tx.MeterStop != 2500 || tx.Ended == nil || !tx.Ended.Equal(time.Date(2026, 1, 1, 0, 2, 0, 0, time.UTC)) {
		t.Fatalf("after end: %+v", tx)
	}
}

func TestOverrideAndObserve(t *testing.T) {
	h := New(Options{Txns: txn.New()})
	h.Override(ocppj.V16, "BootNotification", func(ctx context.Context, call Call) (json.RawMessage, error) {
		return json.RawMessage(`{"currentTime":"2026-01-01T00:00:00Z","interval":60,"status":"Pending"}`), nil
	})
	var seen []string
	h.Observe(func(_ context.Context, call Call, resp json.RawMessage, err error) {
		seen = append(seen, string(call.Version)+" "+call.Action+" "+string(resp))
	})
	_, _ = h.Handle(context.Background(), Call{CPID: "CP1", Version: ocppj.V16, Action: "BootNotification", Payload: json.RawMessage(`{}`)})
	_, _ = h.Handle(context.Background(), Call{CPID: "CP1", Version: ocppj.V201, Action: "Heartbeat", Payload: json.RawMessage(`{}`)})
	if len(seen) != 2 || seen[0] != `1.6 BootNotification {"currentTime":"2026-01-01T00:00:00Z","interval":60,"status":"Pending"}` {
		t.Fatalf("observed %q", seen)
	}
}
