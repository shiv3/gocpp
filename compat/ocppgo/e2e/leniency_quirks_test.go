// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/compat"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
)

const ts = `"2024-01-02T03:04:05Z"`

// Payloads as real chargers send them. Each runs against the default (upstream
// behaviour: rejected with the given CALLERROR code) and with the one flag that
// covers it (accepted).
var quirkTable = []struct {
	name    string
	action  string
	payload string
	flag    compat.Leniency
	reject  string // CALLERROR code by default
	check   func(t *testing.T, h *csHandler)
}{
	{
		name: "empty idTag in Authorize", action: "Authorize", payload: `{"idTag":""}`,
		flag: compat.Leniency{AllowEmptyIdTag: true}, reject: `"OccurrenceConstraintViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if r := h.lastAuth.Load(); r == nil || r.IdTag != "" {
				t.Fatalf("authorize %+v", r)
			}
		},
	},
	{
		name: "empty idTag in StartTransaction", action: "StartTransaction",
		payload: `{"connectorId":1,"idTag":"","meterStart":0,"timestamp":` + ts + `}`,
		flag:    compat.Leniency{AllowEmptyIdTag: true}, reject: `"OccurrenceConstraintViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if h.lastStart.Load() == nil {
				t.Fatal("start not handled")
			}
		},
	},
	{
		name: "empty sampledValue value", action: "MeterValues",
		payload: `{"connectorId":1,"meterValue":[{"timestamp":` + ts + `,"sampledValue":[{"value":""}]}]}`,
		flag:    compat.Leniency{AllowEmptySampledValueValue: true}, reject: `"OccurrenceConstraintViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if r := h.lastMeter.Load(); r == nil || r.MeterValue[0].SampledValue[0].Value != "" {
				t.Fatalf("meter %+v", r)
			}
		},
	},
	{
		name: "empty sampledValue value in StopTransaction", action: "StopTransaction",
		payload: `{"meterStop":1,"timestamp":` + ts + `,"transactionId":1,"transactionData":[{"timestamp":` + ts + `,"sampledValue":[{"value":""}]}]}`,
		flag:    compat.Leniency{AllowEmptySampledValueValue: true}, reject: `"OccurrenceConstraintViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if h.lastStop.Load() == nil {
				t.Fatal("stop not handled")
			}
		},
	},
	{
		name: "vendor measurand and unit", action: "MeterValues",
		payload: `{"connectorId":1,"meterValue":[{"timestamp":` + ts + `,"sampledValue":[{"value":"1","measurand":"Energy.Active.lmport.Register","unit":"kWh2"}]}]}`,
		flag:    compat.Leniency{AllowVendorMeasurands: true}, reject: `"GenericError"`,
		check: func(t *testing.T, h *csHandler) {
			sv := h.lastMeter.Load().MeterValue[0].SampledValue[0]
			if string(sv.Measurand) != "Energy.Active.lmport.Register" || string(sv.Unit) != "kWh2" {
				t.Fatalf("sampled value %+v", sv)
			}
		},
	},
	{
		name: "numeric sampledValue value", action: "MeterValues",
		payload: `{"connectorId":1,"meterValue":[{"timestamp":` + ts + `,"sampledValue":[{"value":12.5},{"value":7}]}]}`,
		flag:    compat.Leniency{AllowNumericSampledValue: true}, reject: `"FormationViolation"`,
		check: func(t *testing.T, h *csHandler) {
			sv := h.lastMeter.Load().MeterValue[0].SampledValue
			if sv[0].Value != "12.5" || sv[1].Value != "7" {
				t.Fatalf("sampled values %+v", sv)
			}
		},
	},
	{
		name: "huge transactionId (Alfen)", action: "MeterValues",
		payload: `{"connectorId":1,"transactionId":9223372036854775807,"meterValue":[{"timestamp":` + ts + `,"sampledValue":[{"value":"1"}]}]}`,
		flag:    compat.Leniency{PreserveLargeIntegers: true}, reject: `"FormationViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if r := h.lastMeter.Load(); r.TransactionId == nil || *r.TransactionId != math.MaxInt64 {
				t.Fatalf("transactionId %v", r.TransactionId)
			}
		},
	},
	{
		name: "timestamp with space and no zone", action: "StartTransaction",
		payload: `{"connectorId":1,"idTag":"a","meterStart":0,"timestamp":"2024-01-02 03:04:05"}`,
		flag:    compat.Leniency{LenientDateTime: true}, reject: `"FormationViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if got := h.lastStart.Load().Timestamp.UTC(); !got.Equal(time.Date(2024, 1, 2, 3, 4, 5, 0, time.UTC)) {
				t.Fatalf("timestamp %v", got)
			}
		},
	},
	{
		name: "timestamp with space and +0100 offset", action: "StartTransaction",
		payload: `{"connectorId":1,"idTag":"a","meterStart":0,"timestamp":"2024-01-02 03:04:05+0100"}`,
		flag:    compat.Leniency{LenientDateTime: true}, reject: `"FormationViolation"`,
		check: func(t *testing.T, h *csHandler) {
			if got := h.lastStart.Load().Timestamp.UTC(); !got.Equal(time.Date(2024, 1, 2, 2, 4, 5, 0, time.UTC)) {
				t.Fatalf("timestamp %v", got)
			}
		},
	},
}

func TestLeniencyQuirks(t *testing.T) {
	for _, tc := range quirkTable {
		t.Run(tc.name+"/default", func(t *testing.T) {
			c, conn := startLenient(t, nil, nil)
			r := callRaw(t, conn, tc.action, tc.payload)
			if string(r[0]) != "4" || string(r[2]) != tc.reject {
				t.Fatalf("default behaviour must match upstream (%s): %s", tc.reject, r)
			}
			if c.h.lastMeter.Load() != nil || c.h.lastStart.Load() != nil || c.h.lastAuth.Load() != nil || c.h.lastStop.Load() != nil {
				t.Fatal("handler must not see the request")
			}
		})
		t.Run(tc.name+"/flag", func(t *testing.T) {
			c, conn := startLenient(t, &tc.flag, nil)
			r := callRaw(t, conn, tc.action, tc.payload)
			if string(r[0]) != "3" {
				t.Fatalf("flag set, reply %s", r)
			}
			tc.check(t, c.h)
		})
	}
}

// A flag only covers its own quirk.
func TestLeniencyQuirkFlagsAreIndependent(t *testing.T) {
	_, conn := startLenient(t, &compat.Leniency{AllowEmptyIdTag: true, AllowVendorMeasurands: true, PreserveLargeIntegers: true}, nil)
	for _, tc := range quirkTable {
		if tc.flag.AllowEmptyIdTag || tc.flag.AllowVendorMeasurands || tc.flag.PreserveLargeIntegers {
			continue
		}
		if r := callRaw(t, conn, tc.action, tc.payload); string(r[0]) != "4" {
			t.Fatalf("%s accepted without its flag: %s", tc.name, r)
		}
	}
	// a repairable timestamp does not hide another violation
	_, conn2 := startLenient(t, &compat.Leniency{LenientDateTime: true}, nil)
	if r := callRaw(t, conn2, "StartTransaction", `{"connectorId":1,"idTag":"a","meterStart":0,"timestamp":"yesterday"}`); string(r[0]) != "4" {
		t.Fatalf("garbage timestamp accepted: %s", r)
	}
}

func TestLeniencyOutboundEmptyValues(t *testing.T) {
	// default: upstream refuses to send
	c, _ := startLenient(t, nil, nil)
	if err := c.cs.ChangeConfiguration("CP-LEN", func(*core.ChangeConfigurationConfirmation, error) {}, "k", ""); err == nil {
		t.Fatal("empty ChangeConfiguration value sent by default")
	}
	if err := c.cs.RemoteStartTransaction("CP-LEN", func(*core.RemoteStartTransactionConfirmation, error) {}, ""); err == nil {
		t.Fatal("empty RemoteStartTransaction idTag sent by default")
	}

	c, conn := startLenient(t, &compat.Leniency{AllowEmptyConfigurationValue: true, AllowEmptyIdTag: true}, nil)
	done := make(chan error, 2)
	if err := c.cs.ChangeConfiguration("CP-LEN", func(_ *core.ChangeConfigurationConfirmation, err error) { done <- err }, "k", ""); err != nil {
		t.Fatalf("ChangeConfiguration with flag: %v", err)
	}
	_, data := readFrame(t, conn)
	if want := `"value":""`; !strings.Contains(string(data), want) {
		t.Fatalf("frame %s", data)
	}
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`[3,`+frameID(data)+`,{"status":"Accepted"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("callback: %v", err)
	}
	if err := c.cs.RemoteStartTransaction("CP-LEN", func(*core.RemoteStartTransactionConfirmation, error) {}, ""); err != nil {
		t.Fatalf("RemoteStartTransaction with flag: %v", err)
	}
	_, data = readFrame(t, conn)
	if !strings.Contains(string(data), `"idTag":""`) {
		t.Fatalf("frame %s", data)
	}
	// other violations of an outbound request are still refused
	if err := c.cs.ChangeConfiguration("CP-LEN", func(*core.ChangeConfigurationConfirmation, error) {}, "", ""); err == nil {
		t.Fatal("empty key sent")
	}
}

func readFrame(t *testing.T, conn *websocket.Conn) (int, []byte) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	mt, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return mt, data
}

func frameID(data []byte) string {
	var arr []json.RawMessage
	_ = json.Unmarshal(data, &arr)
	return string(arr[1])
}
