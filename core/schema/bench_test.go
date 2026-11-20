package schema_test

import (
	"testing"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
)

var benchPayloads = []struct {
	name, action, kind, payload string
}{
	{"HeartbeatReq", "Heartbeat", "request", `{}`},
	{"HeartbeatConf", "Heartbeat", "response", `{"currentTime":"2026-11-21T01:02:03.000Z"}`},
	{"StatusNotificationReq", "StatusNotification", "request",
		`{"connectorId":1,"errorCode":"NoError","status":"Available","timestamp":"2026-11-21T01:02:03Z"}`},
	{"MeterValuesReq", "MeterValues", "request",
		`{"connectorId":1,"transactionId":4242,"meterValue":[{"timestamp":"2026-11-21T01:02:03Z","sampledValue":[` +
			`{"value":"12345","measurand":"Energy.Active.Import.Register","unit":"Wh"},` +
			`{"value":"7.2","measurand":"Power.Active.Import","unit":"kW"},` +
			`{"value":"31.5","measurand":"Current.Import","unit":"A"}]}]}`},
}

func benchRegistry(b *testing.B) *schema.Registry {
	b.Helper()
	reg := schema.NewRegistry()
	if err := v16.RegisterSchemas(reg); err != nil {
		b.Fatal(err)
	}
	return reg
}

// BenchmarkValidateLenient is the per-message cost of the lenient path that
// csms/cp run for every inbound message in lenient mode.
func BenchmarkValidateLenient(b *testing.B) {
	reg := benchRegistry(b)
	for _, p := range benchPayloads {
		b.Run(p.name, func(b *testing.B) {
			v, ok := reg.Lookup("1.6", p.action, p.kind)
			if !ok {
				b.Fatal("no validator")
			}
			raw := []byte(p.payload)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, _, err := v.ValidateLenient(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkValidateStrict is the strict-mode counterpart (Validator.Validate).
func BenchmarkValidateStrict(b *testing.B) {
	reg := benchRegistry(b)
	for _, p := range benchPayloads {
		b.Run(p.name, func(b *testing.B) {
			v, ok := reg.Lookup("1.6", p.action, p.kind)
			if !ok {
				b.Fatal("no validator")
			}
			raw := []byte(p.payload)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := v.Validate(raw); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
