package schema_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v201"
	v201msgs "github.com/shiv3/gocpp/v201/messages"
)

// jsonEqual compares two JSON documents by value (numbers as written).
func jsonEqual(t testing.TB, a, b []byte) bool {
	t.Helper()
	dec := func(x []byte) (any, error) {
		d := json.NewDecoder(bytes.NewReader(x))
		d.UseNumber()
		var v any
		err := d.Decode(&v)
		return v, err
	}
	va, ea := dec(a)
	vb, eb := dec(b)
	if ea != nil || eb != nil {
		return false
	}
	return reflect.DeepEqual(va, vb)
}

// FuzzValidatorInvariants checks properties that must hold for every input and
// every schema, without a second implementation to compare with:
//   - strict and lenient validation never panic;
//   - a payload the strict validator accepts is accepted by lenient mode too,
//     and unchanged in value;
//   - lenient output is valid JSON and lenient mode is idempotent on it.
func FuzzValidatorInvariants(f *testing.F) {
	vs := payloadValidators(f)
	var list []*schema.Validator
	for _, c := range payloadCases {
		if v, ok := vs[[2]string{c.action, c.kind}]; ok {
			list = append(list, v)
		}
	}
	for i, c := range payloadCases {
		if len(c.payload) < 4096 {
			f.Add(uint8(i), []byte(c.payload))
		}
	}
	f.Fuzz(func(t *testing.T, sel uint8, raw []byte) {
		// Payloads reach the validators from ocppj.Parse, which only hands out
		// well-formed JSON; trailing data after the first value is not
		// rejected by the validators themselves.
		if !json.Valid(raw) {
			return
		}
		v := list[int(sel)%len(list)]
		strictErr := v.Validate(raw)
		out, _, err := v.ValidateLenient(raw)
		if strictErr == nil {
			if err != nil {
				t.Fatalf("strict accepts %q but lenient rejects: %v", raw, err)
			}
			if !jsonEqual(t, raw, out) {
				t.Fatalf("strict accepts %q but lenient rewrote it to %q", raw, out)
			}
		}
		if err != nil {
			return
		}
		if !json.Valid(out) {
			t.Fatalf("lenient output for %q is not JSON: %q", raw, out)
		}
		out2, _, err2 := v.ValidateLenient(out)
		if err2 != nil {
			t.Fatalf("lenient output %q (from %q) is rejected by a second pass: %v", out, raw, err2)
		}
		if !jsonEqual(t, out, out2) {
			t.Fatalf("lenient is not idempotent for %q: %q then %q", raw, out, out2)
		}
	})
}

// FuzzTypedDecode: a payload the strict schema accepts must decode into the
// generated Go type, and what the type encodes back must satisfy the schema
// again (schema and generated types agree). OCPP 1.6 and 2.0.1 samples.
func FuzzTypedDecode(f *testing.F) {
	reg := schema.NewRegistry()
	if err := v16.RegisterSchemas(reg); err != nil {
		f.Fatal(err)
	}
	if err := v201.RegisterSchemas(reg); err != nil {
		f.Fatal(err)
	}
	type target struct {
		ver, action, kind string
		newT              func() any
	}
	targets := []target{
		{"1.6", "BootNotification", "request", func() any { return new(messages.BootNotificationRequest) }},
		{"1.6", "BootNotification", "response", func() any { return new(messages.BootNotificationResponse) }},
		{"1.6", "StatusNotification", "request", func() any { return new(messages.StatusNotificationRequest) }},
		{"1.6", "MeterValues", "request", func() any { return new(messages.MeterValuesRequest) }},
		{"1.6", "StartTransaction", "request", func() any { return new(messages.StartTransactionRequest) }},
		{"1.6", "StopTransaction", "request", func() any { return new(messages.StopTransactionRequest) }},
		{"1.6", "SetChargingProfile", "request", func() any { return new(messages.SetChargingProfileRequest) }},
		{"1.6", "GetConfiguration", "response", func() any { return new(messages.GetConfigurationResponse) }},
		{"2.0.1", "BootNotification", "request", func() any { return new(v201msgs.BootNotificationRequest) }},
		{"2.0.1", "TransactionEvent", "request", func() any { return new(v201msgs.TransactionEventRequest) }},
		{"2.0.1", "MeterValues", "request", func() any { return new(v201msgs.MeterValuesRequest) }},
		{"2.0.1", "SetChargingProfile", "request", func() any { return new(v201msgs.SetChargingProfileRequest) }},
		{"2.0.1", "NotifyReport", "request", func() any { return new(v201msgs.NotifyReportRequest) }},
	}
	var vals []*schema.Validator
	for _, tg := range targets {
		v, ok := reg.Lookup(tg.ver, tg.action, tg.kind)
		if !ok {
			f.Fatalf("no schema for %s %s %s", tg.ver, tg.action, tg.kind)
		}
		vals = append(vals, v)
	}
	seeds := []string{
		`{"chargePointVendor":"V","chargePointModel":"M"}`,
		`{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}`,
		`{"connectorId":1,"errorCode":"NoError","status":"Available","timestamp":"2026-01-01T00:00:00Z"}`,
		`{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":"1.5","unit":"kWh"}]}]}`,
		`{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"transactionId":1,"meterStop":10,"timestamp":"2026-01-01T00:00:00Z"}`,
		`{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}}}`,
		`{"configurationKey":[{"key":"k","readonly":false,"value":"v"}],"unknownKey":["x"]}`,
		`{"reason":"PowerUp","chargingStation":{"model":"M","vendorName":"V"}}`,
		`{"eventType":"Started","timestamp":"2026-01-01T00:00:00Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"t"}}`,
		`{"evseId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":1.5}]}]}`,
		`{"evseId":1,"chargingProfile":{"id":1,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":[{"id":1,"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}]}}`,
		`{"requestId":1,"seqNo":0,"generatedAt":"2026-01-01T00:00:00Z","reportData":[{"component":{"name":"c"},"variable":{"name":"v"},"variableAttribute":[{"value":"x"}]}]}`,
	}
	for i := range targets {
		for _, s := range seeds {
			f.Add(uint8(i), []byte(s))
		}
	}
	f.Fuzz(func(t *testing.T, sel uint8, raw []byte) {
		i := int(sel) % len(targets)
		tg, v := targets[i], vals[i]
		if !json.Valid(raw) || v.Validate(raw) != nil {
			return
		}
		msg := tg.newT()
		if err := json.Unmarshal(raw, msg); err != nil {
			t.Fatalf("%s %s %s: schema accepts %q but the generated type rejects it: %v", tg.ver, tg.action, tg.kind, raw, err)
		}
		back, err := json.Marshal(msg)
		if err != nil {
			t.Fatalf("%s %s: marshal of decoded %q: %v", tg.ver, tg.action, raw, err)
		}
		if err := v.Validate(back); err != nil {
			t.Fatalf("%s %s %s: %q decodes and re-encodes to %q, which the schema rejects: %v", tg.ver, tg.action, tg.kind, raw, back, err)
		}
	})
}
