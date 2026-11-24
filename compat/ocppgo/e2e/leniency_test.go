// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/compat"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

func startLenient(t *testing.T, l *compat.Leniency, f compat.ValidationErrorFilter) (*central, *websocket.Conn) {
	t.Helper()
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("validation leniency is a feature of the gocpp runtime")
	}
	srv := ws.NewServer()
	endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
	compat.SetCallTimeout(endpoint, 700*time.Millisecond)
	switch {
	case l != nil:
		if !compat.SetValidationLeniency(endpoint, *l) {
			t.Fatal("SetValidationLeniency refused")
		}
	case f != nil:
		compat.SetValidationErrorFilter(endpoint, f)
	}
	c := &central{h: &csHandler{}, port: freePort(t), srv: srv}
	c.cs = ocpp16.NewCentralSystem(endpoint, srv)
	c.cs.SetCoreHandler(c.h)
	srv.AddHttpHandler("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	go c.cs.Start(c.port, "/{ws}")
	waitFor(t, "listening", func() bool {
		resp, err := healthGet(c.port)
		if err == nil {
			resp.Body.Close()
		}
		return err == nil
	})
	t.Cleanup(srv.Stop)
	conn, _, err := dialCP(c, "CP-LEN")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	waitFor(t, "connected", func() bool { return compat.Connections(srv) == 1 })
	return c, conn
}

// callRaw sends a CALL frame and returns the reply frame.
func callRaw(t *testing.T, conn *websocket.Conn, action, payload string) []json.RawMessage {
	t.Helper()
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`[2,"m1","`+action+`",`+payload+`]`)); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) < 3 {
		t.Fatalf("bad reply %s", data)
	}
	return arr
}

const (
	bootEmptyVendor = `{"chargePointVendor":"","chargePointModel":"m"}`
	meterEmptySV    = `{"connectorId":1,"meterValue":[{"timestamp":"2024-01-01T00:00:00Z","sampledValue":[]}]}`
)

func TestLeniencyDefaultRejects(t *testing.T) {
	c, conn := startLenient(t, nil, nil)
	r := callRaw(t, conn, "BootNotification", bootEmptyVendor)
	if string(r[0]) != "4" || string(r[2]) != `"OccurrenceConstraintViolation"` {
		t.Fatalf("boot reply %s", r)
	}
	r = callRaw(t, conn, "MeterValues", meterEmptySV)
	if string(r[0]) != "4" || string(r[2]) != `"PropertyConstraintViolation"` {
		t.Fatalf("meter reply %s", r)
	}
	if c.h.boots.Load() != 0 || c.h.meters.Load() != 0 {
		t.Fatal("handler must not see the invalid requests")
	}
	if got := answerLongConfig(t, c, conn); got == nil {
		t.Fatal("long configuration value accepted by default")
	}
}

func TestLeniencyPreset(t *testing.T) {
	c, conn := startLenient(t, &compat.Leniency{AllowEmptyChargePointVendor: true, AllowLongConfigurationValues: true, AllowEmptySampledValue: true}, nil)
	if r := callRaw(t, conn, "BootNotification", bootEmptyVendor); string(r[0]) != "3" {
		t.Fatalf("boot reply %s", r)
	}
	if r := callRaw(t, conn, "MeterValues", meterEmptySV); string(r[0]) != "3" {
		t.Fatalf("meter reply %s", r)
	}
	if c.h.boots.Load() != 1 || c.h.meters.Load() != 1 {
		t.Fatal("handler must see both requests")
	}
	if err := answerLongConfig(t, c, conn); err != nil {
		t.Fatalf("long configuration value: %v", err)
	}
	// Other violations stay rejected.
	r := callRaw(t, conn, "BootNotification", `{"chargePointVendor":"`+strings.Repeat("v", 21)+`","chargePointModel":"m"}`)
	if string(r[0]) != "4" {
		t.Fatalf("overlong vendor accepted: %s", r)
	}
	r = callRaw(t, conn, "BootNotification", `{"chargePointVendor":"","chargePointModel":""}`)
	if string(r[0]) != "4" {
		t.Fatalf("empty model accepted: %s", r)
	}
}

func TestLeniencyOnlyWhatIsEnabled(t *testing.T) {
	_, conn := startLenient(t, &compat.Leniency{AllowEmptyChargePointVendor: true}, nil)
	if r := callRaw(t, conn, "MeterValues", meterEmptySV); string(r[0]) != "4" {
		t.Fatalf("empty sampledValue accepted: %s", r)
	}
}

func TestLeniencyCustomFilter(t *testing.T) {
	var seen []string
	c, conn := startLenient(t, nil, func(action, field, tag string) bool {
		seen = append(seen, action+" "+field+" "+tag)
		return action == "BootNotification"
	})
	if r := callRaw(t, conn, "BootNotification", bootEmptyVendor); string(r[0]) != "3" {
		t.Fatalf("boot reply %s", r)
	}
	if r := callRaw(t, conn, "MeterValues", meterEmptySV); string(r[0]) != "4" {
		t.Fatalf("meter reply %s", r)
	}
	if c.h.boots.Load() != 1 {
		t.Fatal("boot not handled")
	}
	want := []string{"BootNotification ChargePointVendor required", "MeterValues MeterValue[0].SampledValue min"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Fatalf("filter saw %q, want %q", seen, want)
	}
}

func TestLeniencySpecSpelling(t *testing.T) {
	_, conn := startLenient(t, &compat.Leniency{SpecSpellingOccurenceV16: true}, nil)
	r := callRaw(t, conn, "BootNotification", bootEmptyVendor)
	if string(r[0]) != "4" || string(r[2]) != `"OccurenceConstraintViolation"` {
		t.Fatalf("boot reply %s", r)
	}
}

// answerLongConfig asks the charger for its configuration, answers with a 600
// character value and returns what the application's callback got.
func answerLongConfig(t *testing.T, c *central, conn *websocket.Conn) error {
	t.Helper()
	got := make(chan error, 1)
	if err := c.cs.GetConfiguration("CP-LEN", func(conf *core.GetConfigurationConfirmation, err error) {
		if err == nil && (len(conf.ConfigurationKey) != 1 || len(*conf.ConfigurationKey[0].Value) != 600) {
			err = ocpp.NewError(ocppj.GenericError, "value lost", "")
		}
		got <- err
	}, nil); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var arr []json.RawMessage
	_ = json.Unmarshal(data, &arr)
	id := string(arr[1])
	frame := `[3,` + id + `,{"configurationKey":[{"key":"k","readonly":false,"value":"` + strings.Repeat("x", 600) + `"}]}]`
	if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-got:
		return e
	case <-time.After(5 * time.Second):
		t.Fatal("callback never invoked")
	}
	return nil
}
