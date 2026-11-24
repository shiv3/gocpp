package main

// The legacy e2e contract: gocpp-csms started with --version X is the CSMS of
// the ocpp-cp-simulator e2e suite, which parses its stdout and drives its
// control endpoints. These tests run the real process (the test binary
// re-executes itself as the command) and pin what that harness relies on:
// the ports sentinel, the NDJSON frame lines, the control envelopes and
// status codes, the default answers on the wire and SIGTERM handling.
//
// Set GOCPP_CSMS_CONTRACT_BIN to run the same checks against another build
// of the binary.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cp"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v201h "github.com/shiv3/gocpp/v201/handlers"
	v201msg "github.com/shiv3/gocpp/v201/messages"
	v21h "github.com/shiv3/gocpp/v21/handlers"
	v21msg "github.com/shiv3/gocpp/v21/messages"
)

const childEnv = "GOCPP_CSMS_CONTRACT_CHILD"

func TestMain(m *testing.M) {
	if os.Getenv(childEnv) == "1" {
		os.Exit(run())
	}
	os.Exit(m.Run())
}

type csmsProcess struct {
	t      *testing.T
	cmd    *exec.Cmd
	ocpp   int
	http   int
	lines  chan string
	stderr *lockedBuffer
	done   chan error
}

type lockedBuffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (l *lockedBuffer) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuffer) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

var sentinelRE = regexp.MustCompile(`^E2E_CSMS_PORTS \{"ocpp":([0-9]+),"http":([0-9]+)\}$`)

func csmsCommand(args ...string) *exec.Cmd {
	if bin := os.Getenv("GOCPP_CSMS_CONTRACT_BIN"); bin != "" {
		return exec.Command(bin, args...)
	}
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	cmd := exec.Command(exe, args...)
	cmd.Env = append(os.Environ(), childEnv+"=1")
	return cmd
}

func startCSMS(t *testing.T, version string) *csmsProcess {
	t.Helper()
	cmd := csmsCommand("--version", version)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &csmsProcess{t: t, cmd: cmd, lines: make(chan string, 1024), stderr: &lockedBuffer{}, done: make(chan error, 1)}
	cmd.Stderr = p.stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() {
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			p.lines <- sc.Text()
		}
		close(p.lines)
		p.done <- cmd.Wait()
	}()
	t.Cleanup(func() {
		if cmd.ProcessState == nil {
			_ = cmd.Process.Kill()
		}
	})

	select {
	case first, ok := <-p.lines:
		if !ok {
			t.Fatalf("process exited before the ports sentinel; stderr:\n%s", p.stderr.String())
		}
		m := sentinelRE.FindStringSubmatch(first)
		if m == nil {
			t.Fatalf("first stdout line = %q, want the E2E_CSMS_PORTS sentinel", first)
		}
		p.ocpp, _ = strconv.Atoi(m[1])
		p.http, _ = strconv.Atoi(m[2])
		if p.ocpp == 0 || p.http == 0 || p.ocpp == p.http {
			t.Fatalf("bad ports in sentinel %q", first)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("no ports sentinel within 10s; stderr:\n%s", p.stderr.String())
	}
	return p
}

// frame is one recorder line.
type frame struct {
	Seq     int64           `json:"seq"`
	CPID    string          `json:"cpId"`
	Action  string          `json:"action"`
	Payload json.RawMessage `json:"payload"`
}

// nextFrame reads the next stdout line and checks it is a recorder line with
// exactly the keys seq, cpId, action, payload in that order.
func (p *csmsProcess) nextFrame() frame {
	p.t.Helper()
	select {
	case line, ok := <-p.lines:
		if !ok {
			p.t.Fatalf("stdout closed while waiting for a frame")
		}
		p.t.Logf("frame %s", line)
		var f frame
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			p.t.Fatalf("stdout line %q is not a frame: %v", line, err)
		}
		want := fmt.Sprintf(`{"seq":%d,"cpId":%q,"action":%q,"payload":`, f.Seq, f.CPID, f.Action)
		if !strings.HasPrefix(line, want) || !strings.HasSuffix(line, "}") {
			p.t.Fatalf("frame line %q does not have the {seq,cpId,action,payload} layout", line)
		}
		return f
	case <-time.After(5 * time.Second):
		p.t.Fatalf("no frame within 5s")
	}
	return frame{}
}

func (p *csmsProcess) expectNoFrame() {
	p.t.Helper()
	select {
	case line, ok := <-p.lines:
		if ok {
			p.t.Fatalf("unexpected stdout line %q", line)
		}
	case <-time.After(200 * time.Millisecond):
	}
}

// do sends a control request and returns the status and the raw body.
func (p *csmsProcess) do(method, path, body string) (int, string) {
	p.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, fmt.Sprintf("http://127.0.0.1:%d%s", p.http, path), rd)
	if err != nil {
		p.t.Fatal(err)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		p.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		p.t.Fatal(err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		p.t.Fatalf("%s %s: Content-Type %q", method, path, ct)
	}
	return resp.StatusCode, string(b)
}

func (p *csmsProcess) expect(method, path, body string, wantStatus int, wantBody string) {
	p.t.Helper()
	status, got := p.do(method, path, body)
	if status != wantStatus || got != wantBody+"\n" {
		p.t.Fatalf("%s %s %s:\n got %d %q\nwant %d %q", method, path, body, status, got, wantStatus, wantBody+"\n")
	}
}

// stop sends SIGTERM and expects a clean exit within 3 s.
func (p *csmsProcess) stop() {
	p.t.Helper()
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		p.t.Fatal(err)
	}
	for range p.lines { //nolint:revive // drain remaining stdout
	}
	select {
	case err := <-p.done:
		if err != nil {
			p.t.Fatalf("exit after SIGTERM: %v; stderr:\n%s", err, p.stderr.String())
		}
	case <-time.After(3 * time.Second):
		p.t.Fatalf("no exit within 3s of SIGTERM")
	}
}

func (p *csmsProcess) dial(t *testing.T, cpID, subprotocol string, register func(*cp.Client) error) *cp.Client {
	t.Helper()
	c := cp.NewClient(cpID, fmt.Sprintf("ws://127.0.0.1:%d/ocpp/%s", p.ocpp, cpID), cp.WithSubProtocols(subprotocol))
	if err := register(c); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if got := c.NegotiatedProtocol(); got != subprotocol {
		t.Fatalf("negotiated %q, want %q", got, subprotocol)
	}
	t.Cleanup(c.Close)
	return c
}

// call sends one CALL with a raw payload, checks the frame the CSMS records
// for it and returns the raw CALLRESULT payload.
func (p *csmsProcess) call(t *testing.T, c *cp.Client, wantSeq int64, action, payload string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cp.CallRaw(ctx, c, action, []byte(payload))
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	t.Logf("answer %s %s", action, resp)
	f := p.nextFrame()
	if f.Seq != wantSeq || f.CPID != "CP1" || f.Action != action {
		t.Fatalf("frame = seq %d cp %q action %q, want seq %d cp CP1 action %s", f.Seq, f.CPID, f.Action, wantSeq, action)
	}
	assertSameJSON(t, action+" payload", string(f.Payload), payload)
	return string(resp)
}

func assertSameJSON(t *testing.T, what, got, want string) {
	t.Helper()
	var g, w any
	if err := json.Unmarshal([]byte(got), &g); err != nil {
		t.Fatalf("%s: %v in %q", what, err, got)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: %v in %q", what, err, want)
	}
	gb, _ := json.Marshal(g)
	wb, _ := json.Marshal(w)
	if string(gb) != string(wb) {
		t.Fatalf("%s:\n got %s\nwant %s", what, gb, wb)
	}
}

var bootRespRE = regexp.MustCompile(`^\{"currentTime":"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z","interval":300,"status":"Accepted"\}$`)
var heartbeatRespRE = regexp.MustCompile(`^\{"currentTime":"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9:.]+Z"\}$`)

const defaultBehavior = `{"ok":true,"result":{"boot":["Accepted"],"bootIntervalSeconds":300,"mode":"accept-all","idTags":[],"rateMax":1,"rateWindowMs":60000}}`

// checkControlBasics pins the control envelopes that do not need a charger.
func checkControlBasics(p *csmsProcess) {
	p.expect(http.MethodGet, "/healthz", "", 200, `{"ok":true}`)
	p.expect(http.MethodPost, "/healthz", "", 405, `{"ok":false,"error":"method not allowed"}`)
	p.expect(http.MethodGet, "/command", "", 405, `{"ok":false,"error":"method not allowed"}`)
	p.expect(http.MethodPost, "/command", `{`, 400, `{"ok":false,"error":"decode command: unexpected end of JSON input"}`)
	p.expect(http.MethodPost, "/command", `{"action":"Reset"}`, 400, `{"ok":false,"error":"cpId is required"}`)
	p.expect(http.MethodPost, "/command", `{"cpId":"CP1"}`, 400, `{"ok":false,"error":"action is required"}`)
	p.expect(http.MethodPost, "/command", `{"cpId":"nobody","action":"Reset"}`, 404, `{"ok":false,"error":"charge point \"nobody\" is not connected"}`)

	p.expect(http.MethodGet, "/behavior", "", 200, defaultBehavior)
	p.expect(http.MethodPost, "/behavior", `{"nope":1}`, 400, `{"ok":false,"error":"decode behavior: json: unknown field \"nope\""}`)
	p.expect(http.MethodPost, "/behavior", `{"mode":"bogus"}`, 400, `{"ok":false,"error":"unsupported mode \"bogus\"; expected accept-all, whitelist, blacklist, rate-limit or offline"}`)
	p.expect(http.MethodPost, "/behavior", `{"bootIntervalSeconds":120}`, 200, `{"ok":true,"result":{"boot":["Accepted"],"bootIntervalSeconds":120,"mode":"accept-all","idTags":[],"rateMax":1,"rateWindowMs":60000}}`)
	p.expect(http.MethodDelete, "/behavior", "", 200, defaultBehavior)
	p.expect(http.MethodPut, "/behavior", "", 405, `{"ok":false,"error":"method not allowed"}`)

	p.expect(http.MethodGet, "/sequence", "", 200, `{"ok":true,"result":[]}`)
	p.expect(http.MethodPost, "/sequence", `{"steps":[]}`, 400, `{"ok":false,"error":"steps must not be empty"}`)
	p.expect(http.MethodPost, "/sequence", `{"steps":[{"command":{}}],"repeat":2}`, 400, `{"ok":false,"error":"repeat needs periodMs"}`)
	p.expect(http.MethodPost, "/sequence", `{"steps":[{"command":{"cpId":"nobody","action":"Reset"}}]}`, 200, `{"ok":true,"result":{"id":1}}`)
	waitFor(p.t, "sequence result", func() bool {
		_, body := p.do(http.MethodGet, "/sequence", "")
		return body == `{"ok":true,"result":[{"id":1,"running":false,"results":[{"round":1,"step":0,"ok":false,"error":"charge point \"nobody\" is not connected"}]}]}`+"\n"
	})
	p.expect(http.MethodDelete, "/sequence", "", 200, `{"ok":true}`)
}

func TestLegacyContractRejectsBadVersion(t *testing.T) {
	for _, args := range [][]string{nil, {"--version", "1.5"}} {
		cmd := csmsCommand(args...)
		var stdout, stderr strings.Builder
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		err := cmd.Run()
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 2 {
			t.Fatalf("args %v: err %v, want exit status 2", args, err)
		}
		if stdout.Len() != 0 {
			t.Fatalf("args %v: stdout %q, want nothing", args, stdout.String())
		}
		if !strings.Contains(stderr.String(), "unsupported --version") {
			t.Fatalf("args %v: stderr %q", args, stderr.String())
		}
	}
}

type cp16 struct{ v16h.UnimplementedCPHandler }

func (cp16) OnRemoteStartTransaction(context.Context, v16msg.RemoteStartTransactionRequest) (v16msg.RemoteStartTransactionResponse, error) {
	return v16msg.RemoteStartTransactionResponse{Status: "Accepted"}, nil
}

func TestLegacyContract16(t *testing.T) {
	p := startCSMS(t, "1.6")
	checkControlBasics(p)
	c := p.dial(t, "CP1", "ocpp1.6", func(c *cp.Client) error { return v16h.RegisterCP(c, cp16{}) })

	if got := p.call(t, c, 1, "BootNotification", `{"chargePointModel":"M","chargePointVendor":"V"}`); !bootRespRE.MatchString(got) {
		t.Fatalf("BootNotification answer %s", got)
	}
	if got := p.call(t, c, 2, "Heartbeat", `{}`); !heartbeatRespRE.MatchString(got) {
		t.Fatalf("Heartbeat answer %s", got)
	}
	expectRaw(t, p.call(t, c, 3, "StatusNotification", `{"connectorId":1,"errorCode":"NoError","status":"Preparing"}`), `{}`)
	expectRaw(t, p.call(t, c, 4, "Authorize", `{"idTag":"TAG1"}`), `{"idTagInfo":{"status":"Accepted"}}`)
	expectRaw(t, p.call(t, c, 5, "StartTransaction", `{"connectorId":1,"idTag":"TAG1","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`), `{"idTagInfo":{"status":"Accepted"},"transactionId":1}`)
	expectRaw(t, p.call(t, c, 6, "MeterValues", `{"connectorId":1,"transactionId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:10Z","sampledValue":[{"value":"1234","measurand":"Energy.Active.Import.Register","unit":"Wh"}]}]}`), `{}`)
	expectRaw(t, p.call(t, c, 7, "StopTransaction", `{"meterStop":1234,"timestamp":"2026-01-01T00:01:00Z","transactionId":1}`), `{}`)
	expectRaw(t, p.call(t, c, 8, "StartTransaction", `{"connectorId":1,"idTag":"TAG2","meterStart":1234,"timestamp":"2026-01-01T00:02:00Z"}`), `{"idTagInfo":{"status":"Accepted"},"transactionId":2}`)
	expectRaw(t, p.call(t, c, 9, "DataTransfer", `{"vendorId":"v"}`), `{"status":"Accepted"}`)

	p.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"RemoteStartTransaction","idTag":"TAG1","connectorId":1}`, 200, `{"ok":true,"result":{"status":"Accepted"}}`)
	// The charger does not implement Reset: its CALLERROR is ok:false at 200.
	status, body := p.do(http.MethodPost, "/command", `{"cpId":"CP1","action":"Reset","type":"Soft"}`)
	if status != 200 || !strings.HasPrefix(body, `{"ok":false,"error":"`) {
		t.Fatalf("Reset on a charger without the handler: %d %s", status, body)
	}
	p.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"Bogus"}`, 200, `{"ok":false,"error":"unsupported 1.6 action \"Bogus\""}`)
	p.expectNoFrame()

	// Authorization policies change the idTagInfo status.
	p.expect(http.MethodPost, "/behavior", `{"mode":"whitelist","idTags":["GOOD"]}`, 200, `{"ok":true,"result":{"boot":["Accepted"],"bootIntervalSeconds":300,"mode":"whitelist","idTags":["GOOD"],"rateMax":1,"rateWindowMs":60000}}`)
	expectRaw(t, p.call(t, c, 10, "Authorize", `{"idTag":"BAD"}`), `{"idTagInfo":{"status":"Invalid"}}`)
	p.expect(http.MethodPost, "/behavior", `{"mode":"blacklist","idTags":["BAD"]}`, 200, `{"ok":true,"result":{"boot":["Accepted"],"bootIntervalSeconds":300,"mode":"blacklist","idTags":["BAD"],"rateMax":1,"rateWindowMs":60000}}`)
	expectRaw(t, p.call(t, c, 11, "Authorize", `{"idTag":"BAD"}`), `{"idTagInfo":{"status":"Blocked"}}`)
	p.expect(http.MethodDelete, "/behavior", "", 200, defaultBehavior)

	// An action without a CSMS handler is answered NotSupported and not recorded.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cp.CallRaw(ctx, c, "FirmwareStatusNotification", []byte(`{"status":"Idle"}`)); err == nil || !strings.Contains(err.Error(), "NotSupported") {
		t.Fatalf("FirmwareStatusNotification: err %v, want NotSupported", err)
	}
	p.expectNoFrame()

	p.stop()
}

type cp201 struct{ v201h.UnimplementedCPHandler }

func (cp201) OnRequestStartTransaction(context.Context, v201msg.RequestStartTransactionRequest) (v201msg.RequestStartTransactionResponse, error) {
	return v201msg.RequestStartTransactionResponse{Status: "Accepted"}, nil
}

type cp21 struct{ v21h.UnimplementedCPHandler }

func (cp21) OnRequestStartTransaction(context.Context, v21msg.RequestStartTransactionRequest) (v21msg.RequestStartTransactionResponse, error) {
	return v21msg.RequestStartTransactionResponse{Status: "Accepted"}, nil
}

func TestLegacyContract2x(t *testing.T) {
	for _, tc := range []struct {
		version, subprotocol string
		register             func(*cp.Client) error
	}{
		{"2.0.1", "ocpp2.0.1", func(c *cp.Client) error { return v201h.RegisterCP(c, cp201{}) }},
		{"2.1", "ocpp2.1", func(c *cp.Client) error { return v21h.RegisterCP(c, cp21{}) }},
	} {
		t.Run(tc.version, func(t *testing.T) {
			p := startCSMS(t, tc.version)
			checkControlBasics(p)
			c := p.dial(t, "CP1", tc.subprotocol, tc.register)

			if got := p.call(t, c, 1, "BootNotification", `{"chargingStation":{"model":"M","vendorName":"V"},"reason":"PowerUp"}`); !bootRespRE.MatchString(got) {
				t.Fatalf("BootNotification answer %s", got)
			}
			if got := p.call(t, c, 2, "Heartbeat", `{}`); !heartbeatRespRE.MatchString(got) {
				t.Fatalf("Heartbeat answer %s", got)
			}
			expectRaw(t, p.call(t, c, 3, "StatusNotification", `{"timestamp":"2026-01-01T00:00:00Z","connectorStatus":"Occupied","evseId":1,"connectorId":1}`), `{}`)
			expectRaw(t, p.call(t, c, 4, "Authorize", `{"idToken":{"idToken":"TAG1","type":"ISO14443"}}`), `{"idTokenInfo":{"status":"Accepted"}}`)
			expectRaw(t, p.call(t, c, 5, "TransactionEvent", `{"eventType":"Started","timestamp":"2026-01-01T00:00:01Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"tx-1"},"idToken":{"idToken":"TAG1","type":"ISO14443"},"evse":{"id":1,"connectorId":1}}`), `{}`)
			expectRaw(t, p.call(t, c, 6, "MeterValues", `{"evseId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:10Z","sampledValue":[{"value":1234.5,"measurand":"Energy.Active.Import.Register"}]}]}`), `{}`)
			expectRaw(t, p.call(t, c, 7, "TransactionEvent", `{"eventType":"Ended","timestamp":"2026-01-01T00:01:00Z","triggerReason":"StopAuthorized","seqNo":1,"transactionInfo":{"transactionId":"tx-1","stoppedReason":"Local"}}`), `{}`)
			expectRaw(t, p.call(t, c, 8, "NotifyReport", `{"requestId":1,"generatedAt":"2026-01-01T00:00:00Z","seqNo":0}`), `{}`)
			expectRaw(t, p.call(t, c, 9, "DataTransfer", `{"vendorId":"v"}`), `{"status":"Accepted"}`)

			p.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"RequestStartTransaction","idToken":{"idToken":"TAG1","type":"ISO14443"},"evseId":1}`, 200, `{"ok":true,"result":{"status":"Accepted"}}`)
			p.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"Bogus"}`, 200, `{"ok":false,"error":"unsupported `+tc.version+` action \"Bogus\""}`)
			p.expectNoFrame()

			// With a policy active, TransactionEvent carries idTokenInfo.
			p.expect(http.MethodPost, "/behavior", `{"mode":"whitelist","idTags":["GOOD"]}`, 200, `{"ok":true,"result":{"boot":["Accepted"],"bootIntervalSeconds":300,"mode":"whitelist","idTags":["GOOD"],"rateMax":1,"rateWindowMs":60000}}`)
			expectRaw(t, p.call(t, c, 10, "TransactionEvent", `{"eventType":"Started","timestamp":"2026-01-01T00:02:00Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"tx-2"},"idToken":{"idToken":"BAD","type":"ISO14443"}}`), `{"idTokenInfo":{"status":"Unknown"}}`)

			p.stop()
		})
	}
}

func expectRaw(t *testing.T, got, want string) {
	t.Helper()
	if got != want {
		t.Fatalf("answer on the wire:\n got %s\nwant %s", got, want)
	}
}
