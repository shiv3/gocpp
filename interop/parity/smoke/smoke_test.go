// Package smoke checks that cs-gocpp and cs-ocppgo answer the same traffic the
// same way. Set PARITY_BIN_DIR to use prebuilt binaries; otherwise TestMain
// builds them into a temp dir.
package smoke

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/coder/websocket"
)

var binDir string

func TestMain(m *testing.M) {
	binDir = os.Getenv("PARITY_BIN_DIR")
	if binDir == "" {
		tmp, err := os.MkdirTemp("", "parity-bin")
		if err != nil {
			panic(err)
		}
		defer os.RemoveAll(tmp)
		binDir = tmp
		for _, mod := range []string{"cs-gocpp", "cs-ocppgo"} {
			cmd := exec.Command("go", "build", "-o", filepath.Join(tmp, mod), ".")
			cmd.Dir = filepath.Join("..", mod)
			if out, err := cmd.CombinedOutput(); err != nil {
				fmt.Fprintf(os.Stderr, "build %s: %v\n%s", mod, err, out)
				os.Exit(1)
			}
		}
	}
	os.Exit(m.Run())
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

type server struct {
	t    *testing.T
	base string
	port int
}

func start(t *testing.T, bin string) *server {
	port := freePort(t)
	events := filepath.Join(t.TempDir(), "events.jsonl")
	cmd := exec.Command(filepath.Join(binDir, bin), "-listen", fmt.Sprintf("127.0.0.1:%d", port), "-events", events, "-call-timeout", "3s")
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill(); _ = cmd.Wait() })
	s := &server{t: t, base: fmt.Sprintf("127.0.0.1:%d", port), port: port}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + s.base + "/control/health")
		if err == nil {
			resp.Body.Close()
			return s
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server did not start")
	return nil
}

func (s *server) post(path string, body any) map[string]any {
	b, _ := json.Marshal(body)
	resp, err := http.Post("http://"+s.base+path, "application/json", bytes.NewReader(b))
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	return readJSON[map[string]any](s.t, resp.Body)
}

func readJSON[T any](t *testing.T, r io.Reader) T {
	var v T
	if err := json.NewDecoder(r).Decode(&v); err != nil {
		t.Fatal(err)
	}
	return v
}

func (s *server) get(path string) any {
	resp, err := http.Get("http://" + s.base + path)
	if err != nil {
		s.t.Fatal(err)
	}
	defer resp.Body.Close()
	return readJSON[any](s.t, resp.Body)
}

func (s *server) dial(ctx context.Context, id string) *websocket.Conn {
	c, _, err := websocket.Dial(ctx, "ws://"+s.base+"/ocpp/"+id, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	if err != nil {
		s.t.Fatal(err)
	}
	return c
}

// call sends a CALL and returns the CALLRESULT payload (decoded).
func call(ctx context.Context, t *testing.T, c *websocket.Conn, id, action string, payload any) any {
	b, _ := json.Marshal([]any{2, id, action, payload})
	if err := c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	_, data, err := c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var frame []any
	if err := json.Unmarshal(data, &frame); err != nil || len(frame) < 3 {
		t.Fatalf("bad frame %s", data)
	}
	if frame[0].(float64) != 3 || frame[1] != id {
		t.Fatalf("%s: not a result for %s: %s", action, id, data)
	}
	return frame[2]
}

func TestParity(t *testing.T) {
	results := map[string]map[string]any{}
	for _, bin := range []string{"cs-gocpp", "cs-ocppgo"} {
		bin := bin
		t.Run(bin, func(t *testing.T) {
			s := start(t, bin)
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c := s.dial(ctx, "CP1")
			defer c.CloseNow()
			out := map[string]any{}
			results[bin] = out

			out["Boot"] = call(ctx, t, c, "1", "BootNotification", map[string]any{"chargePointVendor": "v", "chargePointModel": "m"})
			out["Heartbeat"] = call(ctx, t, c, "2", "Heartbeat", map[string]any{})
			out["AuthOK"] = call(ctx, t, c, "3", "Authorize", map[string]any{"idTag": "TAG1"})
			out["AuthInvalid"] = call(ctx, t, c, "4", "Authorize", map[string]any{"idTag": "INVALID1"})
			out["AuthBlocked"] = call(ctx, t, c, "5", "Authorize", map[string]any{"idTag": "BLOCKED1"})
			out["Start1"] = call(ctx, t, c, "6", "StartTransaction", map[string]any{"connectorId": 1, "idTag": "EXPIRED1", "meterStart": 0, "timestamp": "2026-01-01T00:00:00Z"})
			out["Start2"] = call(ctx, t, c, "7", "StartTransaction", map[string]any{"connectorId": 1, "idTag": "TAG1", "meterStart": 0, "timestamp": "2026-01-01T00:00:00Z"})
			out["Stop"] = call(ctx, t, c, "8", "StopTransaction", map[string]any{"transactionId": 1, "idTag": "TAG1", "meterStop": 5, "timestamp": "2026-01-01T00:00:00Z"})
			out["StopNoTag"] = call(ctx, t, c, "9", "StopTransaction", map[string]any{"transactionId": 1, "meterStop": 5, "timestamp": "2026-01-01T00:00:00Z"})
			out["Status"] = call(ctx, t, c, "10", "StatusNotification", map[string]any{"connectorId": 1, "errorCode": "NoError", "status": "Available"})
			out["DTReject"] = call(ctx, t, c, "11", "DataTransfer", map[string]any{"vendorId": "parity.reject"})
			out["DTEcho"] = call(ctx, t, c, "12", "DataTransfer", map[string]any{"vendorId": "x", "data": "hello"})
			out["DTSleep"] = call(ctx, t, c, "13", "DataTransfer", map[string]any{"vendorId": "parity.sleep", "data": "50"})

			// CSMS-initiated call, answered by the raw client.
			type callRes struct{ r map[string]any }
			done := make(chan callRes, 1)
			go func() {
				done <- callRes{s.post("/control/call", map[string]any{"cpId": "CP1", "action": "GetConfiguration", "payload": map[string]any{}})}
			}()
			_, data, err := c.Read(ctx)
			if err != nil {
				t.Fatal(err)
			}
			var frame []any
			if err := json.Unmarshal(data, &frame); err != nil || len(frame) != 4 || frame[0].(float64) != 2 || frame[2] != "GetConfiguration" {
				t.Fatalf("unexpected CSMS frame %s", data)
			}
			reply, _ := json.Marshal([]any{3, frame[1], map[string]any{"configurationKey": []any{map[string]any{"key": "K", "readonly": false, "value": "V"}}}})
			if err := c.Write(ctx, websocket.MessageText, reply); err != nil {
				t.Fatal(err)
			}
			res := (<-done).r
			if res["ok"] != true {
				t.Fatalf("control call failed: %v", res)
			}
			out["GetConfigurationResult"] = res["result"]

			notConn := s.post("/control/call", map[string]any{"cpId": "NOPE", "action": "Reset", "payload": map[string]any{"type": "Soft"}})
			out["NotConnected"] = notConn["kind"]
			if h := s.get("/control/health").(map[string]any); h["ok"] != true {
				t.Fatalf("health %v", h)
			}
			if conns := s.get("/control/connections"); !reflect.DeepEqual(conns, []any{"CP1"}) {
				t.Fatalf("connections %v", conns)
			}

			evs := s.get("/control/events").([]any)
			var haveConnect, haveBoot bool
			for _, e := range evs {
				m := e.(map[string]any)
				if m["kind"] == "connect" && m["cpId"] == "CP1" {
					haveConnect = true
				}
				if m["kind"] == "request" && m["action"] == "BootNotification" {
					haveBoot = true
				}
			}
			if !haveConnect || !haveBoot {
				t.Fatalf("missing events (connect=%v boot=%v): %v", haveConnect, haveBoot, evs)
			}
		})
	}
	a, b := results["cs-gocpp"], results["cs-ocppgo"]
	if a == nil || b == nil {
		t.Fatal("a subtest failed before producing results")
	}
	for k := range a {
		if !reflect.DeepEqual(a[k], b[k]) {
			t.Errorf("%s differs:\n gocpp:   %v\n ocpp-go: %v", k, a[k], b[k])
		}
	}
}
