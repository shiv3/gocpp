package server

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/inbound"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v201h "github.com/shiv3/gocpp/v201/handlers"
	v201msg "github.com/shiv3/gocpp/v201/messages"
	v21h "github.com/shiv3/gocpp/v21/handlers"
	"golang.org/x/crypto/bcrypt"
)

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

type testServer struct {
	*Server
	t      *testing.T
	stdout *lockedBuffer
	scheme string
}

// defaultConfig returns the flag defaults with ephemeral loopback ports.
func defaultConfig(t *testing.T) *config.Config {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := config.Bind(config.NewBinder(fs, nil))
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	cfg.OCPPAddr = "127.0.0.1:0"
	cfg.ControlAddr = "127.0.0.1:0"
	cfg.DrainTimeout = time.Second
	cfg.CommandTimeout = 3 * time.Second
	return cfg
}

func start(t *testing.T, mutate func(*config.Config), exts ...Extension) *testServer {
	t.Helper()
	cfg := defaultConfig(t)
	if mutate != nil {
		mutate(cfg)
	}
	out := &lockedBuffer{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	s, err := New(cfg, logger, out, exts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			t.Errorf("shutdown: %v", err)
		}
	})
	scheme := "ws"
	if cfg.TLSCert != "" {
		scheme = "wss"
	}
	return &testServer{Server: s, t: t, stdout: out, scheme: scheme}
}

func (s *testServer) url(path string) string {
	return fmt.Sprintf("%s://%s%s", s.scheme, s.OCPPAddr(), path)
}

func (s *testServer) dial(cpID, subprotocol string, register func(*cp.Client) error, opts ...cp.Option) (*cp.Client, error) {
	s.t.Helper()
	opts = append([]cp.Option{cp.WithSubProtocols(subprotocol)}, opts...)
	c := cp.NewClient(cpID, s.url("/ocpp/"+cpID), opts...)
	if register != nil {
		if err := register(c); err != nil {
			s.t.Fatal(err)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return nil, err
	}
	s.t.Cleanup(c.Close)
	return c, nil
}

func (s *testServer) mustDial(cpID, subprotocol string, register func(*cp.Client) error, opts ...cp.Option) *cp.Client {
	s.t.Helper()
	c, err := s.dial(cpID, subprotocol, register, opts...)
	if err != nil {
		s.t.Fatalf("connect %s (%s): %v", cpID, subprotocol, err)
	}
	if got := c.NegotiatedProtocol(); got != subprotocol {
		s.t.Fatalf("negotiated %q, want %q", got, subprotocol)
	}
	return c
}

func (s *testServer) control(method, path, body string) (int, string) {
	s.t.Helper()
	req, err := http.NewRequest(method, "http://"+s.ControlAddr().String()+path, strings.NewReader(body))
	if err != nil {
		s.t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSuffix(string(b), "\n")
}

func (s *testServer) expect(method, path, body string, wantStatus int, wantBody string) {
	s.t.Helper()
	status, got := s.control(method, path, body)
	if status != wantStatus || got != wantBody {
		s.t.Fatalf("%s %s %s\n got %d %s\nwant %d %s", method, path, body, status, got, wantStatus, wantBody)
	}
}

func callRaw(t *testing.T, c *cp.Client, action, payload string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cp.CallRaw(ctx, c, action, []byte(payload))
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return string(resp)
}

type cp16 struct{ v16h.UnimplementedCPHandler }

func (cp16) OnReset(context.Context, v16msg.ResetRequest) (v16msg.ResetResponse, error) {
	return v16msg.ResetResponse{Status: "Accepted"}, nil
}

type cp201 struct{ v201h.UnimplementedCPHandler }

func (cp201) OnGetVariables(_ context.Context, req v201msg.GetVariablesRequest) (v201msg.GetVariablesResponse, error) {
	out := v201msg.GetVariablesResponse{}
	for _, d := range req.GetVariableData {
		v := "42"
		out.GetVariableResult = append(out.GetVariableResult, v201msg.GetVariableResultType{
			AttributeStatus: "Accepted", AttributeValue: &v, Component: d.Component, Variable: d.Variable,
		})
	}
	return out, nil
}

type cp21 struct{ v21h.UnimplementedCPHandler }

// TestMultiVersionOnOnePort connects a 1.6, a 2.0.1 and a 2.1 charge point
// to the same listener and exercises each with inbound and outbound calls.
func TestMultiVersionOnOnePort(t *testing.T) {
	s := start(t, func(c *config.Config) { c.Record = true })
	c16 := s.mustDial("CP16", "ocpp1.6", func(c *cp.Client) error { return v16h.RegisterCP(c, cp16{}) })
	c201 := s.mustDial("CP201", "ocpp2.0.1", func(c *cp.Client) error { return v201h.RegisterCP(c, cp201{}) })
	c21 := s.mustDial("CP21", "ocpp2.1", func(c *cp.Client) error { return v21h.RegisterCP(c, cp21{}) })

	if got := callRaw(t, c16, "BootNotification", `{"chargePointModel":"M","chargePointVendor":"V"}`); !strings.Contains(got, `"interval":300,"status":"Accepted"`) {
		t.Fatalf("1.6 boot: %s", got)
	}
	boot2x := `{"chargingStation":{"model":"M","vendorName":"V"},"reason":"PowerUp"}`
	if got := callRaw(t, c201, "BootNotification", boot2x); !strings.Contains(got, `"status":"Accepted"`) {
		t.Fatalf("2.0.1 boot: %s", got)
	}
	if got := callRaw(t, c21, "BootNotification", boot2x); !strings.Contains(got, `"status":"Accepted"`) {
		t.Fatalf("2.1 boot: %s", got)
	}

	_, body := s.control(http.MethodGet, "/connections", "")
	var conns struct {
		Result []struct{ CPID, Version string }
	}
	if err := json.Unmarshal([]byte(body), &conns); err != nil || len(conns.Result) != 3 {
		t.Fatalf("/connections: %s", body)
	}
	versions := map[string]string{}
	for _, c := range conns.Result {
		versions[c.CPID] = c.Version
	}
	if versions["CP16"] != "1.6" || versions["CP201"] != "2.0.1" || versions["CP21"] != "2.1" {
		t.Fatalf("versions %v", versions)
	}

	// Each charge point is commanded in its own version.
	s.expect(http.MethodPost, "/command", `{"cpId":"CP16","action":"Reset","payload":{"type":"Soft"}}`, 200, `{"ok":true,"result":{"status":"Accepted"}}`)
	s.expect(http.MethodPost, "/command", `{"cpId":"CP201","action":"GetVariables","payload":{"getVariableData":[{"component":{"name":"OCPPCommCtrlr"},"variable":{"name":"HeartbeatInterval"}}]}}`, 200,
		`{"ok":true,"result":{"getVariableResult":[{"attributeStatus":"Accepted","attributeValue":"42","component":{"name":"OCPPCommCtrlr"},"variable":{"name":"HeartbeatInterval"}}]}}`)

	// Recorder lines: same shape as the legacy recorder, seq from 1.
	waitFor(t, "three recorder lines", func() bool { return strings.Count(s.stdout.String(), "\n") == 3 })
	sc := bufio.NewScanner(strings.NewReader(s.stdout.String()))
	seen := map[string]bool{}
	for i := int64(1); sc.Scan(); i++ {
		var f struct {
			Seq     int64           `json:"seq"`
			CPID    string          `json:"cpId"`
			Action  string          `json:"action"`
			Payload json.RawMessage `json:"payload"`
		}
		if err := json.Unmarshal(sc.Bytes(), &f); err != nil || f.Seq != i || f.Action != "BootNotification" {
			t.Fatalf("line %d: %s (%v)", i, sc.Text(), err)
		}
		if !strings.HasPrefix(sc.Text(), `{"seq":`) {
			t.Fatalf("key order: %s", sc.Text())
		}
		seen[f.CPID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("recorded %v", seen)
	}
}

func TestVersionsFlagLimitsSubprotocols(t *testing.T) {
	s := start(t, func(c *config.Config) { c.Versions = []string{"2.0.1"} })
	if _, err := s.dial("CP1", "ocpp1.6", nil); err == nil {
		t.Fatal("1.6 accepted with --versions 2.0.1")
	}
	s.mustDial("CP2", "ocpp2.0.1", nil)
}

func TestPathPrefixLastSegment(t *testing.T) {
	s := start(t, func(c *config.Config) { c.PathPrefix = "/csms/" })
	c := cp.NewClient("CP9", s.url("/csms/site-a/CP9"), cp.WithSubProtocols("ocpp1.6"))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	waitFor(t, "CP9 connected", func() bool {
		_, body := s.control(http.MethodGet, "/connections", "")
		return strings.Contains(body, `"cpId":"CP9"`)
	})
	bad := cp.NewClient("CPX", s.url("/ocpp/CPX"), cp.WithSubProtocols("ocpp1.6"))
	if err := bad.Connect(ctx); err == nil {
		bad.Close()
		t.Fatal("connected outside the path prefix")
	}
}

func TestBasicAuth(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("hashed-pw"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "basic")
	if err := os.WriteFile(file, []byte("CP1:plain-pw\nCP2:"+string(hash)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := start(t, func(c *config.Config) { c.BasicAuthFile = file })

	s.mustDial("CP1", "ocpp1.6", nil, cp.WithBasicAuth("CP1", "plain-pw"))
	s.mustDial("CP2", "ocpp2.0.1", nil, cp.WithBasicAuth("CP2", "hashed-pw"))
	for _, tc := range []struct {
		name string
		opts []cp.Option
	}{
		{"CP1", []cp.Option{cp.WithBasicAuth("CP1", "wrong")}},
		{"CP1", nil},
		{"CP3", []cp.Option{cp.WithBasicAuth("CP3", "plain-pw")}},
	} {
		if _, err := s.dial(tc.name, "ocpp1.6", nil, tc.opts...); err == nil || !strings.Contains(err.Error(), "401") {
			t.Fatalf("%s %v: err %v, want a 401 refusal", tc.name, tc.opts, err)
		}
	}
}

// pki is a throw-away CA with a server and a client certificate.
type pki struct {
	dir                 string
	pool                *x509.CertPool
	serverCert, keyFile string
	caFile              string
	client              func(cn string) tls.Certificate
}

func newPKI(t *testing.T) *pki {
	t.Helper()
	dir := t.TempDir()
	caKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	caTmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign,
	}
	caDER, err := x509.CreateCertificate(rand.Reader, caTmpl, caTmpl, &caKey.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	caCert, _ := x509.ParseCertificate(caDER)
	pool := x509.NewCertPool()
	pool.AddCert(caCert)
	issue := func(serial int64, cn string, server bool) ([]byte, *ecdsa.PrivateKey) {
		key, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		tmpl := &x509.Certificate{
			SerialNumber: big.NewInt(serial), Subject: pkix.Name{CommonName: cn},
			NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
			KeyUsage: x509.KeyUsageDigitalSignature,
		}
		if server {
			tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		} else {
			tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
		}
		der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
		if err != nil {
			t.Fatal(err)
		}
		return der, key
	}
	write := func(name, typ string, der []byte) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: typ, Bytes: der}), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	srvDER, srvKey := issue(2, "127.0.0.1", true)
	srvKeyDER, _ := x509.MarshalECPrivateKey(srvKey)
	p := &pki{dir: dir, pool: pool}
	p.serverCert = write("server.pem", "CERTIFICATE", srvDER)
	p.keyFile = write("server-key.pem", "EC PRIVATE KEY", srvKeyDER)
	p.caFile = write("ca.pem", "CERTIFICATE", caDER)
	serial := int64(10)
	p.client = func(cn string) tls.Certificate {
		serial++
		der, key := issue(serial, cn, false)
		return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	}
	return p
}

func TestTLS(t *testing.T) {
	p := newPKI(t)
	s := start(t, func(c *config.Config) { c.TLSCert, c.TLSKey = p.serverCert, p.keyFile })
	c := s.mustDial("CP1", "ocpp2.1", nil, cp.WithTLSConfig(&tls.Config{RootCAs: p.pool, MinVersion: tls.VersionTLS12}))
	if got := callRaw(t, c, "Heartbeat", `{}`); !strings.HasPrefix(got, `{"currentTime":"`) {
		t.Fatalf("heartbeat over TLS: %s", got)
	}
}

func TestMutualTLS(t *testing.T) {
	p := newPKI(t)
	s := start(t, func(c *config.Config) {
		c.TLSCert, c.TLSKey, c.TLSClientCA, c.MTLSMatchCPID = p.serverCert, p.keyFile, p.caFile, true
	})
	withCert := func(cn string) cp.Option {
		return cp.WithTLSConfig(&tls.Config{RootCAs: p.pool, MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{p.client(cn)}})
	}
	s.mustDial("CP1", "ocpp1.6", nil, withCert("CP1"))
	if _, err := s.dial("CP2", "ocpp1.6", nil, withCert("CP1")); err == nil {
		t.Fatal("certificate of CP1 accepted for CP2")
	}
	if _, err := s.dial("CP3", "ocpp1.6", nil, cp.WithTLSConfig(&tls.Config{RootCAs: p.pool, MinVersion: tls.VersionTLS12})); err == nil {
		t.Fatal("connected without a client certificate")
	}
}

func TestGenericCommand(t *testing.T) {
	s := start(t, nil)
	s.mustDial("CP1", "ocpp1.6", func(c *cp.Client) error { return v16h.RegisterCP(c, cp16{}) })

	s.expect(http.MethodGet, "/command", "", 405, `{"ok":false,"error":"method not allowed"}`)
	s.expect(http.MethodPost, "/command", `{`, 400, `{"ok":false,"error":"decode command: unexpected end of JSON input"}`)
	s.expect(http.MethodPost, "/command", `{"action":"Reset"}`, 400, `{"ok":false,"error":"cpId is required"}`)
	s.expect(http.MethodPost, "/command", `{"cpId":"nobody","action":"Reset","payload":{"type":"Soft"}}`, 404, `{"ok":false,"error":"charge point \"nobody\" is not connected"}`)
	// Flat legacy shape works too.
	s.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"Reset","type":"Hard"}`, 200, `{"ok":true,"result":{"status":"Accepted"}}`)
	// Schema refusal: nothing is sent.
	status, body := s.control(http.MethodPost, "/command", `{"cpId":"CP1","action":"Reset","payload":{"type":"Sideways"}}`)
	if status != 400 || !strings.HasPrefix(body, `{"ok":false,"error":"payload does not match the 1.6 Reset request schema: `) {
		t.Fatalf("invalid payload: %d %s", status, body)
	}
	status, body = s.control(http.MethodPost, "/command", `{"cpId":"CP1","action":"Reset","payload":{}}`)
	if status != 400 || !strings.Contains(body, "request schema") {
		t.Fatalf("missing required field: %d %s", status, body)
	}
	s.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"RequestStartTransaction","payload":{}}`, 400, `{"ok":false,"error":"unsupported 1.6 action \"RequestStartTransaction\""}`)
	s.expect(http.MethodPost, "/command", `{"cpId":"CP1","action":"BootNotification","payload":{}}`, 400, `{"ok":false,"error":"unsupported 1.6 action \"BootNotification\""}`)
	// A valid action the charger does not implement: CALLERROR, ok:false at 200.
	status, body = s.control(http.MethodPost, "/command", `{"cpId":"CP1","action":"ClearCache","payload":{}}`)
	if status != 200 || !strings.HasPrefix(body, `{"ok":false,"error":"`) || !strings.HasSuffix(body, `"errorCode":"NotSupported"}`) {
		t.Fatalf("unimplemented on the charger: %d %s", status, body)
	}
}

// TestInboundAcceptAll sends a sample of charge-point actions of each
// version over the wire and checks the answers.
func TestInboundAcceptAll(t *testing.T) {
	s := start(t, nil)
	c16 := s.mustDial("CP16", "ocpp1.6", nil)
	c201 := s.mustDial("CP201", "ocpp2.0.1", nil)
	c21 := s.mustDial("CP21", "ocpp2.1", nil)
	for _, tc := range []struct {
		c               *cp.Client
		action, payload string
		want            string
	}{
		{c16, "FirmwareStatusNotification", `{"status":"Installed"}`, `{}`},
		{c16, "DiagnosticsStatusNotification", `{"status":"Uploaded"}`, `{}`},
		{c16, "SecurityEventNotification", `{"type":"StartupOfTheDevice","timestamp":"2026-01-01T00:00:00Z"}`, `{}`},
		{c16, "SignCertificate", `{"csr":"-----BEGIN CERTIFICATE REQUEST-----"}`, `{"status":"Accepted"}`},
		{c16, "LogStatusNotification", `{"status":"Uploaded"}`, `{}`},
		{c16, "SignedFirmwareStatusNotification", `{"status":"Installed"}`, `{}`},
		{c16, "Authorize", `{"idTag":"ANY"}`, `{"idTagInfo":{"status":"Accepted"}}`},
		{c201, "NotifyEvent", `{"generatedAt":"2026-01-01T00:00:00Z","seqNo":0,"eventData":[{"eventId":1,"timestamp":"2026-01-01T00:00:00Z","trigger":"Alerting","actualValue":"1","eventNotificationType":"HardWiredNotification","component":{"name":"C"},"variable":{"name":"V"}}]}`, `{}`},
		{c201, "ReportChargingProfiles", `{"requestId":1,"chargingLimitSource":"CSO","evseId":1,"chargingProfile":[{"id":1,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":[{"id":1,"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16}]}]}]}`, `{}`},
		{c201, "NotifyEVChargingNeeds", `{"evseId":1,"chargingNeeds":{"requestedEnergyTransfer":"AC_single_phase"}}`, `{"status":"Accepted"}`},
		{c201, "Get15118EVCertificate", `{"iso15118SchemaVersion":"urn:iso:15118:2:2013:MsgDef","action":"Install","exiRequest":"AA=="}`, `{"status":"Accepted","exiResponse":""}`},
		{c201, "Authorize", `{"idToken":{"idToken":"ANY","type":"ISO14443"}}`, `{"idTokenInfo":{"status":"Accepted"}}`},
		{c21, "NotifyChargingLimit", `{"chargingLimit":{"chargingLimitSource":"EMS"}}`, `{}`},
		{c21, "NotifyPriorityCharging", `{"transactionId":"t","activated":true}`, `{}`},
		{c21, "VatNumberValidation", `{"vatNumber":"NL1"}`, `{"status":"Accepted","vatNumber":"NL1"}`},
		{c21, "PullDynamicScheduleUpdate", `{"chargingProfileId":1}`, `{"status":"Accepted"}`},
	} {
		if got := callRaw(t, tc.c, tc.action, tc.payload); got != tc.want {
			t.Errorf("%s: %s, want %s", tc.action, got, tc.want)
		}
	}
	// A schema-invalid request is refused by strict validation.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cp.CallRaw(ctx, c16, "StatusNotification", []byte(`{"connectorId":"one"}`)); err == nil {
		t.Fatal("invalid StatusNotification accepted")
	}
}

func TestTransactionIDs(t *testing.T) {
	s := start(t, nil)
	a := s.mustDial("CPA", "ocpp1.6", nil)
	b := s.mustDial("CPB", "ocpp1.6", nil)
	c := s.mustDial("CPC", "ocpp2.0.1", nil)
	start16 := func(c *cp.Client, conn int) string {
		return callRaw(t, c, "StartTransaction", fmt.Sprintf(`{"connectorId":%d,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`, conn))
	}
	if got := start16(a, 1); got != `{"idTagInfo":{"status":"Accepted"},"transactionId":1}` {
		t.Fatal(got)
	}
	if got := start16(b, 1); got != `{"idTagInfo":{"status":"Accepted"},"transactionId":2}` {
		t.Fatal(got)
	}
	if got := start16(a, 2); got != `{"idTagInfo":{"status":"Accepted"},"transactionId":3}` {
		t.Fatal(got)
	}
	callRaw(t, a, "StopTransaction", `{"transactionId":1,"meterStop":500,"timestamp":"2026-01-01T00:05:00Z","reason":"EVDisconnected"}`)
	callRaw(t, c, "TransactionEvent", `{"eventType":"Started","timestamp":"2026-01-01T00:00:00Z","triggerReason":"CablePluggedIn","seqNo":0,"transactionInfo":{"transactionId":"abc"},"evse":{"id":1,"connectorId":1}}`)

	_, body := s.control(http.MethodGet, "/transactions?active=true", "")
	var resp struct {
		Result []struct {
			CPID string `json:"cpId"`
			ID   string `json:"transactionId"`
		}
	}
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, tx := range resp.Result {
		got = append(got, tx.CPID+"/"+tx.ID)
	}
	if strings.Join(got, ",") != "CPB/2,CPA/3,CPC/abc" {
		t.Fatalf("active transactions %v (%s)", got, body)
	}
	_, body = s.control(http.MethodGet, "/transactions?cpId=CPA", "")
	if !strings.Contains(body, `"transactionId":"1"`) || !strings.Contains(body, `"stoppedReason":"EVDisconnected"`) || !strings.Contains(body, `"meterStop":500`) {
		t.Fatalf("CPA transactions: %s", body)
	}
	s.expect(http.MethodGet, "/transactions?active=maybe", "", 400, `{"ok":false,"error":"active must be \"true\" or \"false\""}`)
}

func TestHealthReadyMetricsAndDrain(t *testing.T) {
	s := start(t, func(c *config.Config) { c.DrainTimeout = 2 * time.Second })
	s.expect(http.MethodGet, "/healthz", "", 200, `{"ok":true}`)
	s.expect(http.MethodPost, "/healthz", "", 405, `{"ok":false,"error":"method not allowed"}`)
	s.expect(http.MethodGet, "/readyz", "", 200, `ok`)
	disconnected := make(chan struct{})
	var once sync.Once
	c := s.mustDial("CP1", "ocpp1.6", nil, cp.WithOnDisconnect(func(error) { once.Do(func() { close(disconnected) }) }))
	callRaw(t, c, "Heartbeat", `{}`)
	status, body := s.control(http.MethodGet, "/metrics", "")
	if status != 200 || !strings.Contains(body, `gocpp_connections{version="ocpp1.6"} 1`) || !strings.Contains(body, "go_goroutines") {
		t.Fatalf("/metrics: %d\n%s", status, body)
	}

	done := make(chan error, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		done <- s.Shutdown(ctx)
	}()
	select {
	case <-disconnected:
	case <-time.After(5 * time.Second):
		t.Fatal("charge point not disconnected by the drain")
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestMaxConnections(t *testing.T) {
	s := start(t, func(c *config.Config) { c.MaxConnections = 1 })
	s.mustDial("CP1", "ocpp1.6", nil)
	if _, err := s.dial("CP2", "ocpp1.6", nil); err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("second connection: %v, want 503", err)
	}
}

// testExt exercises every extension hook.
type testExt struct {
	started  chan *Runtime
	frames   chan string
	connects chan string
}

func (e *testExt) Name() string           { return "test" }
func (e *testExt) Flags(b *config.Binder) {}
func (e *testExt) Setup(h *Hooks) error {
	h.Inbound.Override(ocppj.V16, "BootNotification", func(ctx context.Context, call inbound.Call) (json.RawMessage, error) {
		return json.RawMessage(`{"currentTime":"2026-01-01T00:00:00Z","interval":10,"status":"Pending"}`), nil
	})
	h.FrameObserver(func(_ context.Context, ev observability.FrameEvent) {
		select {
		case e.frames <- ev.Action:
		default:
		}
	})
	h.OnConnect(func(c *csms.Conn) { e.connects <- c.ID() })
	h.Handle("GET /ext/hello", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("hi")) }))
	h.OnStart(func(ctx context.Context, rt *Runtime) error {
		e.started <- rt
		return nil
	})
	return nil
}

func TestExtensionHooks(t *testing.T) {
	ext := &testExt{started: make(chan *Runtime, 1), frames: make(chan string, 16), connects: make(chan string, 1)}
	s := start(t, nil, ext)
	rt := <-ext.started
	if rt.Dispatcher == nil || rt.CSMS == nil {
		t.Fatal("runtime incomplete")
	}
	s.expect(http.MethodGet, "/ext/hello", "", 200, "hi")
	c := s.mustDial("CP1", "ocpp1.6", nil)
	if id := <-ext.connects; id != "CP1" {
		t.Fatal(id)
	}
	if got := callRaw(t, c, "BootNotification", `{"chargePointModel":"M","chargePointVendor":"V"}`); got != `{"currentTime":"2026-01-01T00:00:00Z","interval":10,"status":"Pending"}` {
		t.Fatalf("override: %s", got)
	}
	select {
	case a := <-ext.frames:
		if a != "BootNotification" {
			t.Fatalf("first frame %q", a)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no frame observed")
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
