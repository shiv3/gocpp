// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"encoding/json"
	"net/http"
	"os"
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

// callbackCentral starts a central system with NO disconnect handler registered
// (the handler is optional in ocpp-go's API) and a short call timeout.
func callbackCentral(t *testing.T, configure func(*ocppj.Server)) (*central, *ocppj.Server) {
	t.Helper()
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("uses compat.SetCallTimeout, a feature of the gocpp runtime")
	}
	srv := ws.NewServer()
	endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
	compat.SetCallTimeout(endpoint, 800*time.Millisecond)
	if configure != nil {
		configure(endpoint)
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
	return c, endpoint
}

func readCallID(t *testing.T, conn *websocket.Conn) string {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) < 2 || string(arr[0]) != "2" {
		t.Fatalf("not a call frame: %s", data)
	}
	var id string
	_ = json.Unmarshal(arr[1], &id)
	return id
}

// TestPendingCallbackFailedOnDisconnectWithoutHandler: a call is pending when
// the charge point drops; it reconnects and answers a different call. ocpp-go
// (without a disconnect handler) hands that answer to the callback of the first
// call. Here the first callback fails at the disconnect and the second one gets
// its own answer.
func TestPendingCallbackFailedOnDisconnectWithoutHandler(t *testing.T) {
	c, _ := callbackCentral(t, nil)
	conn, _, err := dialCP(c, "CP-DROP")
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "connected", func() bool { return compat.Connections(c.srv) == 1 })
	first := make(chan error, 1)
	if err := c.cs.SendRequestAsync("CP-DROP", core.NewChangeAvailabilityRequest(1, core.AvailabilityTypeOperative), func(r ocpp.Response, err error) { first <- err }); err != nil {
		t.Fatal(err)
	}
	readCallID(t, conn)
	conn.Close() // drops without answering
	select {
	case err := <-first:
		if err == nil {
			t.Fatal("first callback was called without an error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("first callback was not completed by the disconnect")
	}

	waitFor(t, "disconnected", func() bool { return compat.Connections(c.srv) == 0 })
	conn, _, err = dialCP(c, "CP-DROP")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitFor(t, "reconnected", func() bool { return compat.Connections(c.srv) == 1 })
	type res struct {
		r   ocpp.Response
		err error
	}
	second := make(chan res, 1)
	if err := c.cs.SendRequestAsync("CP-DROP", core.NewGetConfigurationRequest(nil), func(r ocpp.Response, err error) { second <- res{r, err} }); err != nil {
		t.Fatal(err)
	}
	id := readCallID(t, conn)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`[3,"`+id+`",{"configurationKey":[]}]`))
	select {
	case got := <-second:
		if _, ok := got.r.(*core.GetConfigurationConfirmation); !ok || got.err != nil {
			t.Fatalf("second callback got %T, %v", got.r, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second callback was not called")
	}
}

// TestCallErrorGoesToItsOwnCallback: two calls are outstanding on one charge
// point; the first is answered with a CALLERROR, the second with a result. Each
// callback must get its own.
func TestCallErrorGoesToItsOwnCallback(t *testing.T) {
	c, _ := callbackCentral(t, nil)
	conn, _, err := dialCP(c, "CP-ERR")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitFor(t, "connected", func() bool { return compat.Connections(c.srv) == 1 })
	type res struct {
		r   ocpp.Response
		err error
	}
	a, b := make(chan res, 1), make(chan res, 1)
	if err := c.cs.SendRequestAsync("CP-ERR", core.NewResetRequest(core.ResetTypeSoft), func(r ocpp.Response, err error) { a <- res{r, err} }); err != nil {
		t.Fatal(err)
	}
	if err := c.cs.SendRequestAsync("CP-ERR", core.NewClearCacheRequest(), func(r ocpp.Response, err error) { b <- res{r, err} }); err != nil {
		t.Fatal(err)
	}
	id := readCallID(t, conn)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`[4,"`+id+`","NotSupported","no reset",{}]`))
	id = readCallID(t, conn)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`[3,"`+id+`",{"status":"Accepted"}]`))
	select {
	case got := <-a:
		if got.r != nil || got.err == nil {
			t.Fatalf("Reset callback got %T, %v, want the CALLERROR", got.r, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Reset callback not called")
	}
	select {
	case got := <-b:
		if _, ok := got.r.(*core.ClearCacheConfirmation); !ok || got.err != nil {
			t.Fatalf("ClearCache callback got %T, %v", got.r, got.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("ClearCache callback not called")
	}
}

// answerGetConfigurationReadonlyString answers a GetConfiguration call with
// "readonly":"true" (a string, as upstream issue #234 reports) and returns when
// the callback was called, with how long it took.
func answerReadonlyString(t *testing.T, c *central, id string) (error, time.Duration) {
	t.Helper()
	conn, _, err := dialCP(c, id)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitFor(t, "connected", func() bool { return compat.Connections(c.srv) == 1 })
	got := make(chan error, 1)
	start := time.Now()
	if err := c.cs.GetConfiguration(id, func(conf *core.GetConfigurationConfirmation, err error) { got <- err }, nil); err != nil {
		t.Fatal(err)
	}
	cid := readCallID(t, conn)
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`[3,"`+cid+`",{"configurationKey":[{"key":"k","readonly":"true","value":"v"}]}]`))
	select {
	case err := <-got:
		return err, time.Since(start)
	case <-time.After(5 * time.Second):
		t.Fatal("callback never called")
	}
	return nil, 0
}

// TestUnparsableAnswerDefaultWaitsForTimeout: ocpp-go's behaviour (default): the
// callback hears about an answer that cannot be parsed when the call times out.
func TestUnparsableAnswerDefaultWaitsForTimeout(t *testing.T) {
	c, _ := callbackCentral(t, nil)
	err, took := answerReadonlyString(t, c, "CP-234")
	if err == nil || took < 600*time.Millisecond {
		t.Fatalf("callback after %v with %v, want the timeout error", took, err)
	}
}

// TestUnparsableAnswerToCallbackOptIn: with compat.SetValidationErrorToCallback
// the callback gets the error at once (upstream issue #234).
func TestUnparsableAnswerToCallbackOptIn(t *testing.T) {
	c, _ := callbackCentral(t, func(e *ocppj.Server) {
		if !compat.SetValidationErrorToCallback(e, true) {
			t.Fatal("not on the gocpp runtime")
		}
	})
	err, took := answerReadonlyString(t, c, "CP-234")
	if err == nil || took > 400*time.Millisecond {
		t.Fatalf("callback after %v with %v, want an error well before the 800 ms timeout", took, err)
	}
}
