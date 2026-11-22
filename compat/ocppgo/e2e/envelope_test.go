// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
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

// TestEnvelopeErrors sends frames that are malformed at the OCPP-J level and
// checks what ocpp-go answers for them: the error code and description of
// ocpp-go's own parser, and a call of InvalidMessageHook for each of them (the
// hook may also replace the error).
func TestEnvelopeErrors(t *testing.T) {
	type hookCall struct {
		code, desc, id string
		fields         int
	}
	var mu sync.Mutex
	var hooks []hookCall
	replace := false

	srv := ws.NewServer()
	endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
	endpoint.SetInvalidMessageHook(func(_ ws.Channel, e *ocpp.Error, raw string, fields []interface{}) *ocpp.Error {
		mu.Lock()
		defer mu.Unlock()
		hooks = append(hooks, hookCall{string(e.Code), e.Description, e.MessageId, len(fields)})
		if replace {
			return ocpp.NewError(ocppj.GenericError, "replaced by hook", "ignored")
		}
		return nil
	})
	c := &central{h: &csHandler{}, port: freePort(t), srv: srv}
	c.cs = ocpp16.NewCentralSystem(endpoint, srv)
	c.cs.SetCoreHandler(c.h)
	srv.AddHttpHandler("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	go c.cs.Start(c.port, "/{ws}")
	for i := 0; i < 250; i++ {
		time.Sleep(20 * time.Millisecond)
		if resp, err := healthGet(c.port); err == nil {
			resp.Body.Close()
			break
		}
	}
	t.Cleanup(srv.Stop)

	conn, _, err := websocket.DefaultDialer.Dial(fmt.Sprintf("ws://127.0.0.1:%d/CP-ENV", c.port),
		http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()

	// roundTrip sends a frame and then a Heartbeat as a marker, so that "no
	// answer" is distinguished from a slow one.
	n := 0
	roundTrip := func(frame string) string {
		t.Helper()
		if err := conn.WriteMessage(websocket.TextMessage, []byte(frame)); err != nil {
			t.Fatal(err)
		}
		n++
		// The error reply is queued on its own goroutine: let it go out before the marker.
		time.Sleep(100 * time.Millisecond)
		marker := fmt.Sprintf("mk%d", n)
		if err := conn.WriteMessage(websocket.TextMessage, []byte(`[2,"`+marker+`","Heartbeat",{}]`)); err != nil {
			t.Fatal(err)
		}
		var first string
		for {
			_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("frame %s: %v", frame, err)
			}
			s := string(data)
			if len(s) > 4 && s[:4] == `[3,"` && len(s) > 5+len(marker) && s[4:4+len(marker)] == marker {
				return first
			}
			first = s
		}
	}

	cases := []struct {
		frame, want string
		hook        hookCall
		noHook      bool
	}{
		{`[2,"i1","Heartbeat"]`, `[4,"i1","FormationViolation","Invalid Call message. Expected array length 4",{}]`,
			hookCall{"FormationViolation", "Invalid Call message. Expected array length 4", "i1", 3}, false},
		{`[2,"i2","Heartbeat",{},1]`, `[4,"i2","FormationViolation","Invalid Call message. Expected array length 4",{}]`,
			hookCall{"FormationViolation", "Invalid Call message. Expected array length 4", "i2", 5}, false},
		{`[9,"i3",{}]`, `[4,"i3","MessageTypeNotSupported","Invalid message type ID 9",{}]`,
			hookCall{"MessageTypeNotSupported", "Invalid message type ID 9", "i3", 3}, false},
		{`[2,"i4",5,{}]`, ``, hookCall{"FormationViolation", "Invalid element 5 at 2, expected action (string)", "", 4}, false},
		{`[2,"i5"]`, ``, hookCall{"FormationViolation", "Invalid message. Expected array length >= 3", "", 2}, false},
		{`[2,7,"Heartbeat",{}]`, ``, hookCall{"FormationViolation", "Invalid element 7 at 1, expected unique ID (string)", "", 4}, false},
		{`["x","i6","Heartbeat",{}]`, ``, hookCall{"FormationViolation", "Invalid element x at 0, expected message type (int)", "", 4}, false},
		{`{"not":"an array"}`, ``, hookCall{}, true},
		{`[4,"nobody","GenericError","d",{}]`, ``, hookCall{}, true}, // answer to nothing: discarded, no hook
	}
	for _, tc := range cases {
		mu.Lock()
		hooks = nil
		mu.Unlock()
		got := roundTrip(tc.frame)
		if got != tc.want {
			t.Errorf("%s: answered %q, want %q", tc.frame, got, tc.want)
		}
		mu.Lock()
		h := append([]hookCall(nil), hooks...)
		mu.Unlock()
		switch {
		case tc.noHook && len(h) != 0:
			t.Errorf("%s: hook called %v, want not called", tc.frame, h)
		case !tc.noHook && (len(h) != 1 || h[0] != tc.hook):
			t.Errorf("%s: hook calls %v, want [%v]", tc.frame, h, tc.hook)
		}
	}

	// A hook that returns an error replaces the answer; the message id stays the original one.
	mu.Lock()
	replace = true
	mu.Unlock()
	if got, want := roundTrip(`[2,"r1","Heartbeat"]`), `[4,"r1","GenericError","replaced by hook",{}]`; got != want {
		t.Errorf("replaced: answered %q, want %q", got, want)
	}
	// No id known, so nothing is sent, replaced or not.
	if got := roundTrip(`[2,"r2",5,{}]`); got != "" {
		t.Errorf("replaced without id: answered %q", got)
	}
}

// TestInvalidAnswer: a CALLRESULT that fails validation is handled as ocpp-go
// does: InvalidMessageHook sees it, the charger is sent the error with the
// request's id, and the application hears about the request only at its timeout.
func TestInvalidAnswer(t *testing.T) {
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("uses compat.SetCallTimeout, a feature of the gocpp runtime")
	}
	var mu sync.Mutex
	var hooked []string
	srv := ws.NewServer()
	endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
	endpoint.SetInvalidMessageHook(func(_ ws.Channel, e *ocpp.Error, raw string, _ []interface{}) *ocpp.Error {
		mu.Lock()
		hooked = append(hooked, string(e.Code)+" "+raw)
		mu.Unlock()
		return nil
	})
	compat.SetCallTimeout(endpoint, 800*time.Millisecond)
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

	conn, _, err := dialCP(c, "CP-BAD")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	waitFor(t, "connected", func() bool { return compat.Connections(srv) == 1 })
	got := make(chan error, 1)
	start := time.Now()
	if err := c.cs.SendRequestAsync("CP-BAD", core.NewRemoteStartTransactionRequest("TAG"), func(_ ocpp.Response, err error) { got <- err }); err != nil {
		t.Fatal(err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	var arr []json.RawMessage
	if err := json.Unmarshal(data, &arr); err != nil || len(arr) < 2 {
		t.Fatalf("bad call frame %s", data)
	}
	_ = json.Unmarshal(arr[1], &id)
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`[3,"`+id+`",{"status":"Bogus"}]`)); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var back []json.RawMessage
	if err := json.Unmarshal(data, &back); err != nil || len(back) < 4 || string(back[0]) != "4" || string(back[1]) != `"`+id+`"` {
		t.Fatalf("charger got %s, want a CALLERROR for %s", data, id)
	}
	select {
	case err := <-got:
		if err == nil || time.Since(start) < 600*time.Millisecond {
			t.Fatalf("callback after %v with %v, want the timeout error", time.Since(start), err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback never invoked")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(hooked) != 1 {
		t.Fatalf("hook calls %v, want 1", hooked)
	}
}
