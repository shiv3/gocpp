// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package e2e

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/compat"
	ocpp16 "github.com/lorenzodonini/ocpp-go/ocpp1.6"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

type external struct {
	srv *ws.Server
	cs  ocpp16.CentralSystem
	h   *csHandler
	ts  *httptest.Server
}

// startExternal serves the central system through compat.Handler on a mux that
// also has an unrelated route, as an application with gin or chi would.
func startExternal(t *testing.T, configure func(*ws.Server)) *external {
	t.Helper()
	if os.Getenv("OCPPGO_COMPAT_RUNTIME") == "legacy" {
		t.Skip("server helpers need the gocpp runtime")
	}
	srv := ws.NewServer()
	endpoint := ocppj.NewServer(srv, nil, nil, core.Profile)
	if configure != nil {
		configure(srv)
	}
	e := &external{srv: srv, h: &csHandler{}}
	e.cs = ocpp16.NewCentralSystem(endpoint, srv)
	e.cs.SetCoreHandler(e.h)
	if !compat.StartWithoutListener(endpoint, srv) {
		t.Fatal("StartWithoutListener refused")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/ping", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("pong")) })
	mux.Handle("/ocpp/", compat.Handler(srv))
	e.ts = httptest.NewServer(mux)
	t.Cleanup(e.ts.Close)
	t.Cleanup(e.cs.Stop)
	return e
}

func (e *external) dial(t *testing.T, path string, d *websocket.Dialer, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	if d == nil {
		d = websocket.DefaultDialer
	}
	h := http.Header{"Sec-WebSocket-Protocol": []string{"ocpp1.6"}}
	for k, v := range hdr {
		h[k] = v
	}
	conn, resp, err := d.Dial("ws"+strings.TrimPrefix(e.ts.URL, "http")+path, h)
	if err == nil {
		t.Cleanup(func() { conn.Close() })
	}
	return conn, resp, err
}

func TestHandlerOnExternalMux(t *testing.T) {
	e := startExternal(t, nil)
	if resp, err := http.Get(e.ts.URL + "/api/ping"); err != nil || resp.StatusCode != 200 {
		t.Fatalf("other route: %v %v", resp, err)
	}
	b, _, err := e.dial(t, "/ocpp/CP-B", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	a, _, err := e.dial(t, "/ocpp/CP-A", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two connections", func() bool { return len(compat.ConnectionIDs(e.srv)) == 2 })
	if got := strings.Join(compat.ConnectionIDs(e.srv), ","); got != "CP-A,CP-B" {
		t.Fatalf("ids %q", got)
	}
	if r := callRaw(t, a, "BootNotification", `{"chargePointVendor":"v","chargePointModel":"m"}`); string(r[0]) != "3" {
		t.Fatalf("boot %s", r)
	}
	// server initiated request on the mounted connection
	done := make(chan error, 1)
	if err := e.cs.ChangeAvailability("CP-B", func(_ *core.ChangeAvailabilityConfirmation, err error) { done <- err }, 1, core.AvailabilityTypeOperative); err != nil {
		t.Fatal(err)
	}
	data := readCall(t, b)
	if err := b.WriteMessage(websocket.TextMessage, []byte(`[3,`+callID(data)+`,{"status":"Accepted"}]`)); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatalf("callback: %v", err)
	}
	// Stop closes the connections although the server never listened
	e.cs.Stop()
	_ = a.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, _, err := a.ReadMessage(); err == nil {
		t.Fatal("connection still open after Stop")
	}
	waitFor(t, "table emptied", func() bool { return len(compat.ConnectionIDs(e.srv)) == 0 })
}

func TestBasicAuthSeesChargePointID(t *testing.T) {
	seen := make(chan string, 4)
	e := startExternal(t, func(s *ws.Server) {
		compat.SetBasicAuthIDHandler(s, func(id, user, pass string) bool {
			seen <- id + "/" + user
			return user == id && pass == "secret"
		})
	})
	_, resp, err := e.dial(t, "/ocpp/CP-1", nil, nil)
	if err == nil || resp == nil || resp.StatusCode != 401 || resp.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("no credentials: %v %v", resp, err)
	}
	hdr := http.Header{"Authorization": []string{"Basic " + basic("CP-2", "secret")}}
	_, resp, err = e.dial(t, "/ocpp/CP-1", nil, hdr)
	if err == nil || resp.StatusCode != 401 {
		t.Fatalf("credentials of another charge point accepted: %v %v", resp, err)
	}
	if got := <-seen; got != "CP-1/CP-2" {
		t.Fatalf("handler saw %q", got)
	}
	hdr = http.Header{"Authorization": []string{"Basic " + basic("CP-1", "secret")}}
	conn, _, err := e.dial(t, "/ocpp/CP-1", nil, hdr)
	if err != nil {
		t.Fatalf("valid credentials: %v", err)
	}
	if r := callRaw(t, conn, "Heartbeat", `{}`); string(r[0]) != "3" {
		t.Fatalf("heartbeat %s", r)
	}
}

func TestDefaultBasicAuthHandlerUnchanged(t *testing.T) {
	e := startExternal(t, func(s *ws.Server) {
		s.SetBasicAuthHandler(func(user, pass string) bool { return user == "u" && pass == "p" })
	})
	if _, resp, err := e.dial(t, "/ocpp/CP-1", nil, nil); err == nil || resp.StatusCode != 401 {
		t.Fatalf("%v %v", resp, err)
	}
	if _, _, err := e.dial(t, "/ocpp/CP-1", nil, http.Header{"Authorization": []string{"Basic " + basic("u", "p")}}); err != nil {
		t.Fatal(err)
	}
}

func TestIDResolverNestedPath(t *testing.T) {
	e := startExternal(t, func(s *ws.Server) {
		compat.SetIDResolver(s, func(r *http.Request, pathID string) (string, bool) {
			parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/ocpp/"), "/"), "/")
			if len(parts) != 2 {
				return "", false
			}
			return parts[0] + "-" + parts[1], true
		})
	})
	if _, resp, err := e.dial(t, "/ocpp/lonely", nil, nil); err == nil || resp.StatusCode != 404 {
		t.Fatalf("one segment: %v %v", resp, err)
	}
	conn, _, err := e.dial(t, "/ocpp/org1/CP9", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r := callRaw(t, conn, "Heartbeat", `{}`); string(r[0]) != "3" {
		t.Fatalf("heartbeat %s", r)
	}
	if got := strings.Join(compat.ConnectionIDs(e.srv), ","); got != "org1-CP9" {
		t.Fatalf("ids %q", got)
	}
}

func TestCompressionOptIn(t *testing.T) {
	d := &websocket.Dialer{EnableCompression: true}
	ext := func(resp *http.Response) string { return resp.Header.Get("Sec-WebSocket-Extensions") }

	e := startExternal(t, nil)
	conn, resp, err := e.dial(t, "/ocpp/CP-1", d, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := ext(resp); got != "" {
		t.Fatalf("compression negotiated by default: %q", got)
	}
	_ = conn

	for _, m := range []compat.Compression{compat.CompressionContextTakeover, compat.CompressionNoContextTakeover} {
		e = startExternal(t, func(s *ws.Server) { compat.SetCompression(s, m) })
		conn, resp, err = e.dial(t, "/ocpp/CP-1", d, nil)
		if err != nil {
			t.Fatal(err)
		}
		if got := ext(resp); !strings.Contains(got, "permessage-deflate") {
			t.Fatalf("mode %d: extensions %q", m, got)
		}
		conn.EnableWriteCompression(true)
		big := `{"chargePointVendor":"v","chargePointModel":"m","chargePointSerialNumber":"` + strings.Repeat("a", 20) + `"}`
		if r := callRaw(t, conn, "BootNotification", big); string(r[0]) != "3" {
			t.Fatalf("boot over compressed connection: %s", r)
		}
	}
}

func TestConnTimeouts(t *testing.T) {
	e := startExternal(t, func(s *ws.Server) {
		compat.SetConnTimeouts(s, func(r *http.Request, id string) compat.ConnTimeouts {
			if id == "CP-FAST" {
				return compat.ConnTimeouts{CallTimeout: 300 * time.Millisecond, IdleTimeout: 700 * time.Millisecond}
			}
			return compat.ConnTimeouts{}
		})
	})
	slow, _, err := e.dial(t, "/ocpp/CP-SLOW", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.dial(t, "/ocpp/CP-FAST", nil, nil); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "both connected", func() bool { return len(compat.ConnectionIDs(e.srv)) == 2 })
	// per-connection call timeout: the charger never answers
	start := time.Now()
	done := make(chan error, 1)
	if err := e.cs.ChangeAvailability("CP-FAST", func(_ *core.ChangeAvailabilityConfirmation, err error) { done <- err }, 1, core.AvailabilityTypeOperative); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil || time.Since(start) > 3*time.Second {
			t.Fatalf("callback: %v after %v", err, time.Since(start))
		}
	case <-time.After(5 * time.Second):
		t.Fatal("per-connection call timeout not applied")
	}
	// per-connection idle timeout: CP-FAST is dropped, CP-SLOW stays
	waitFor(t, "idle connection dropped", func() bool { return len(compat.ConnectionIDs(e.srv)) == 1 })
	if got := compat.ConnectionIDs(e.srv); got[0] != "CP-SLOW" {
		t.Fatalf("remaining %v", got)
	}
	if r := callRaw(t, slow, "Heartbeat", `{}`); string(r[0]) != "3" {
		t.Fatalf("heartbeat %s", r)
	}
}

func basic(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))
}

// readCall reads the next frame the central system sent and returns it.
func readCall(t *testing.T, conn *websocket.Conn) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// callID is the message id of a CALL frame, as raw JSON.
func callID(data []byte) string {
	var arr []json.RawMessage
	_ = json.Unmarshal(data, &arr)
	return string(arr[1])
}
