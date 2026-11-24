// r09: cost of ocpp-go's inbound decode path (JSON -> []interface{} /
// map[string]interface{} -> json.Marshal -> json.Unmarshal into reflect.New)
// against decoding the same frame once into the same typed struct, with the
// same validator. Only the decode strategy differs.
package r09

import (
	"encoding/json"
	"testing"

	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
)

var frames = map[string]string{
	"Heartbeat":          `[2,"1001","Heartbeat",{}]`,
	"StatusNotification": `[2,"1002","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Charging","timestamp":"2026-11-24T00:00:00Z","vendorId":"acme"}]`,
	"MeterValues8": `[2,"1003","MeterValues",{"connectorId":1,"transactionId":4711,"meterValue":[{"timestamp":"2026-11-24T00:00:00Z","sampledValue":[` +
		`{"value":"123456","measurand":"Energy.Active.Import.Register","unit":"Wh","context":"Sample.Periodic"},` +
		`{"value":"7400","measurand":"Power.Active.Import","unit":"W","context":"Sample.Periodic"},` +
		`{"value":"16.0","measurand":"Current.Import","unit":"A","phase":"L1","context":"Sample.Periodic"},` +
		`{"value":"16.1","measurand":"Current.Import","unit":"A","phase":"L2","context":"Sample.Periodic"},` +
		`{"value":"15.9","measurand":"Current.Import","unit":"A","phase":"L3","context":"Sample.Periodic"},` +
		`{"value":"230.1","measurand":"Voltage","unit":"V","phase":"L1","context":"Sample.Periodic"},` +
		`{"value":"54","measurand":"SoC","unit":"Percent","context":"Sample.Periodic"},` +
		`{"value":"32","measurand":"Current.Offered","unit":"A","context":"Sample.Periodic"}]}]}]`,
}

func endpoint() *ocppj.Server {
	s := ocppj.NewServer(nil, nil, nil, core.Profile)
	s.SetDialect(ocpp.V16)
	return s
}

func BenchmarkOcppGoParse(b *testing.B) {
	s := endpoint()
	st := ocppj.NewClientState()
	for name, f := range frames {
		data := []byte(f)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				arr, err := ocppj.ParseRawJsonMessage(data)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := s.ParseMessage(arr, st); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkSingleTypedDecode(b *testing.B) {
	mk := map[string]func() ocpp.Request{
		"Heartbeat":          func() ocpp.Request { return &core.HeartbeatRequest{} },
		"StatusNotification": func() ocpp.Request { return &core.StatusNotificationRequest{} },
		"MeterValues8":       func() ocpp.Request { return &core.MeterValuesRequest{} },
	}
	for name, f := range frames {
		data := []byte(f)
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				var env []json.RawMessage
				if err := json.Unmarshal(data, &env); err != nil || len(env) != 4 {
					b.Fatal(err)
				}
				var id, action string
				_ = json.Unmarshal(env[1], &id)
				_ = json.Unmarshal(env[2], &action)
				req := mk[name]()
				if err := json.Unmarshal(env[3], req); err != nil {
					b.Fatal(err)
				}
				call := ocppj.Call{MessageTypeId: ocppj.CALL, UniqueId: id, Action: action, Payload: req}
				if err := ocppj.Validate.Struct(call); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
