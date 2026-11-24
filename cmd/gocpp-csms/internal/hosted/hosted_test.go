package hosted

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/controlauth"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/servertest"
	"github.com/shiv3/gocpp/cp"
	"golang.org/x/crypto/bcrypt"
)

func TestMain(m *testing.M) {
	bcryptCost = bcrypt.MinCost
	os.Exit(m.Run())
}

func start(t *testing.T, args ...string) (*servertest.Server, *Extension) {
	t.Helper()
	e := NewExtension()
	return servertest.Start(t, append([]string{"--hosted"}, args...), e), e
}

type envelope[T any] struct {
	OK     bool   `json:"ok"`
	Result T      `json:"result"`
	Error  string `json:"error"`
}

func decodeResult[T any](t *testing.T, body string) T {
	t.Helper()
	var env envelope[T]
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return env.Result
}

func expectStatus(t *testing.T, s *servertest.Server, method, path, body string, want int) string {
	t.Helper()
	status, got := s.Control(method, path, body)
	if status != want {
		t.Fatalf("%s %s %s: %d %s, want %d", method, path, body, status, got, want)
	}
	return got
}

func TestOpenModeAcceptsAnyAndRefusesRegistration(t *testing.T) {
	s, _ := start(t, "--hosted-public-url", "ws://csms.test/ocpp/")
	s.MustDial("ANY1", "/ocpp/ANY1")
	s.MustDial("ANY2", "/ocpp/site/ANY2")
	expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP1","displayName":"one"}`, http.StatusConflict)
	info := decodeResult[InstanceInfo](t, expectStatus(t, s, http.MethodGet, "/instance", "", 200))
	if info.Mode != ModeOpen || info.ConnectionLimit != nil || info.WSBaseURL != "ws://csms.test/ocpp/" {
		t.Fatalf("instance %+v", info)
	}
	servertest.WaitFor(t, "2 active connections", func() bool {
		return decodeResult[InstanceInfo](t, expectStatus(t, s, http.MethodGet, "/instance", "", 200)).ActiveConnections == 2
	})
}

func TestConnectionLimit429(t *testing.T) {
	s, _ := start(t, "--hosted-connection-limit", "1")
	s.MustDial("CP1", "/ocpp/CP1")
	_, err := s.Dial("CP2", "/ocpp/CP2")
	if err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("over the limit: %v, want 429", err)
	}
	// The connected id replaces its own session without a slot.
	s.MustDial("CP1", "/ocpp/CP1")

	s0, _ := start(t, "--hosted-connection-limit", "0")
	if _, err := s0.Dial("CP1", "/ocpp/CP1"); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("limit 0: %v, want 429", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://"+s0.OCPPAddr().String()+"/ocpp/CP1", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests || resp.Header.Get("Retry-After") == "" {
		t.Fatalf("got %d Retry-After %q", resp.StatusCode, resp.Header.Get("Retry-After"))
	}
}

func TestPrivateModeRegistrationLifecycle(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	s, _ := start(t, "--hosted-mode", "private", "--hosted-state-file", state, "--hosted-public-url", "wss://csms.test/ocpp")

	expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP1"}`, http.StatusBadRequest)
	expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"a/b","displayName":"x"}`, http.StatusBadRequest)
	creds := decodeResult[Credentials](t, expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP1","displayName":"Garage"}`, http.StatusCreated))
	if creds.CPID != "CP1" || creds.WSURL != "wss://csms.test/ocpp/CP1" || len(creds.Password) < 16 || len(creds.Password) > 40 {
		t.Fatalf("credentials %+v", creds)
	}
	expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP1","displayName":"again"}`, http.StatusConflict)

	if _, err := s.Dial("CP1", "/ocpp/CP1"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("no password: %v, want 401", err)
	}
	if _, err := s.Dial("CP1", "/ocpp/CP1", cp.WithBasicAuth("CP1", "wrong")); err == nil {
		t.Fatal("wrong password accepted")
	}
	if _, err := s.Dial("CP9", "/ocpp/CP9"); err == nil || !strings.Contains(err.Error(), "401") {
		t.Fatalf("unregistered: %v, want 401", err)
	}
	first := s.MustDial("CP1", "/ocpp/CP1", cp.WithBasicAuth("CP1", creds.Password))
	servertest.Call(t, first, "Heartbeat", `{}`)

	list := decodeResult[[]RegistrationInfo](t, expectStatus(t, s, http.MethodGet, "/registrations", "", 200))
	if len(list) != 1 || list[0].DisplayName != "Garage" || !list[0].Connected {
		t.Fatalf("list %+v", list)
	}
	if b, _ := os.ReadFile(state); strings.Contains(string(b), creds.Password) || !strings.Contains(string(b), `"passwordHash"`) {
		t.Fatalf("state file holds the password or no hash:\n%s", b)
	}

	// Rotation: the old password stops working, the live session stays.
	rotated := decodeResult[Credentials](t, expectStatus(t, s, http.MethodPost, "/registrations/CP1/password", "", 200))
	if rotated.Password == creds.Password {
		t.Fatal("password not rotated")
	}
	if _, err := s.Dial("CP1", "/ocpp/CP1", cp.WithBasicAuth("CP1", creds.Password)); err == nil {
		t.Fatal("old password accepted after rotation")
	}
	live := s.MustDial("CP1", "/ocpp/CP1", cp.WithBasicAuth("CP1", rotated.Password))
	expectStatus(t, s, http.MethodPost, "/registrations/NOPE/password", "", http.StatusNotFound)

	// Delete does not drop the live session; the next connection is refused.
	expectStatus(t, s, http.MethodDelete, "/registrations/NOPE", "", http.StatusNotFound)
	expectStatus(t, s, http.MethodDelete, "/registrations/CP1", "", 200)
	servertest.Call(t, live, "Heartbeat", `{}`)
	if _, err := s.Dial("CP1", "/ocpp/CP1", cp.WithBasicAuth("CP1", rotated.Password)); err == nil {
		t.Fatal("deleted charge point accepted")
	}

	// Clear and mode switch.
	decodeResult[Credentials](t, expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP2","displayName":"two"}`, http.StatusCreated))
	decodeResult[Credentials](t, expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP3","displayName":"three"}`, http.StatusCreated))
	if got := expectStatus(t, s, http.MethodPost, "/instance/clear", "", 200); got != `{"ok":true,"result":{"deleted":2}}` {
		t.Fatal(got)
	}
	expectStatus(t, s, http.MethodPut, "/instance/mode", `{"mode":"bogus"}`, http.StatusBadRequest)
	info := decodeResult[InstanceInfo](t, expectStatus(t, s, http.MethodPut, "/instance/mode", `{"mode":"open"}`, 200))
	if info.Mode != ModeOpen {
		t.Fatalf("%+v", info)
	}
	s.MustDial("CP9", "/ocpp/CP9")
}

func TestPersistenceSurvivesRestart(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	s, _ := start(t, "--hosted-mode", "private", "--hosted-state-file", state)
	creds := decodeResult[Credentials](t, expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"CP1","displayName":"one"}`, http.StatusCreated))
	s.Stop()

	// No --hosted-mode: the persisted private mode applies.
	s2, _ := start(t, "--hosted-state-file", state)
	if m := decodeResult[InstanceInfo](t, expectStatus(t, s2, http.MethodGet, "/instance", "", 200)).Mode; m != ModePrivate {
		t.Fatalf("mode after restart %q", m)
	}
	s2.MustDial("CP1", "/ocpp/CP1", cp.WithBasicAuth("CP1", creds.Password))
	if _, err := s2.Dial("CP2", "/ocpp/CP2"); err == nil {
		t.Fatal("unregistered accepted after restart")
	}
	// Registrations are kept while open and apply again in private mode.
	expectStatus(t, s2, http.MethodPut, "/instance/mode", `{"mode":"open"}`, 200)
	s2.Stop()
	reg, err := Open(state)
	if err != nil {
		t.Fatal(err)
	}
	if reg.Mode() != ModeOpen || !reg.Verify("CP1", creds.Password) || reg.Verify("CP1", "x") {
		t.Fatalf("reloaded registry: mode %s", reg.Mode())
	}
	if fi, err := os.Stat(state); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode: %v %v", fi.Mode(), err)
	}
	if m, _ := filepath.Glob(filepath.Join(filepath.Dir(state), ".*tmp*")); len(m) > 0 {
		t.Fatalf("temporary files left: %v", m)
	}
}

func TestInstancePath(t *testing.T) {
	state := filepath.Join(t.TempDir(), "state.json")
	s, e := start(t, "--hosted-instance-path", "--hosted-state-file", state, "--hosted-public-url", "wss://csms.test/ocpp/")
	id := e.Instance().instanceID
	if len(id) < 20 {
		t.Fatalf("generated instance id %q", id)
	}
	info := decodeResult[InstanceInfo](t, expectStatus(t, s, http.MethodGet, "/instance", "", 200))
	if info.InstanceID != id || info.WSBaseURL != "wss://csms.test/ocpp/"+id+"/" {
		t.Fatalf("%+v", info)
	}
	s.MustDial("CP1", "/ocpp/"+id+"/CP1")
	if _, err := s.Dial("CP2", "/ocpp/CP2"); err == nil {
		t.Fatal("path without instance id accepted")
	}
	if _, err := s.Dial("CP2", "/ocpp/wrong/CP2"); err == nil {
		t.Fatal("wrong instance id accepted")
	}
	s.Stop()
	_, e2 := start(t, "--hosted-instance-path", "--hosted-state-file", state)
	if e2.Instance().instanceID != id {
		t.Fatal("instance id not persisted")
	}
}

func TestPrivateModeStaticFileFallback(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "chargers.txt")
	if err := os.WriteFile(file, []byte("STATIC:secret-static-pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, _ := start(t, "--hosted-mode", "private", "--basic-auth-file", file)
	s.MustDial("STATIC", "/ocpp/STATIC", cp.WithBasicAuth("STATIC", "secret-static-pw"))
	if _, err := s.Dial("OTHER", "/ocpp/OTHER", cp.WithBasicAuth("OTHER", "secret-static-pw")); err == nil {
		t.Fatal("unlisted charge point accepted")
	}
	creds := decodeResult[Credentials](t, expectStatus(t, s, http.MethodPost, "/registrations", `{"cpId":"STATIC","displayName":"s"}`, http.StatusCreated))
	// The registration wins over the static entry.
	if _, err := s.Dial("STATIC", "/ocpp/STATIC", cp.WithBasicAuth("STATIC", "secret-static-pw")); err == nil {
		t.Fatal("static password accepted for a registered id")
	}
	s.MustDial("STATIC", "/ocpp/STATIC", cp.WithBasicAuth("STATIC", creds.Password))
}

func TestFlagsNeedHosted(t *testing.T) {
	_, _, err := servertest.New(t, []string{"--hosted-mode", "private"}, NewExtension())
	if err == nil || !strings.Contains(err.Error(), "--hosted") {
		t.Fatalf("got %v", err)
	}
}

func TestControlToken(t *testing.T) {
	s := servertest.Start(t, []string{"--hosted", "--control-token", "s3cret"}, controlauth.NewExtension(), NewExtension())
	for _, path := range []string{"/instance", "/connections", "/metrics"} {
		if status, _ := s.Control(http.MethodGet, path, ""); status != http.StatusUnauthorized {
			t.Fatalf("%s without token: %d", path, status)
		}
		if status, _ := s.Control(http.MethodGet, path, "", "Authorization: Bearer nope"); status != http.StatusUnauthorized {
			t.Fatalf("%s with a wrong token: %d", path, status)
		}
		if status, _ := s.Control(http.MethodGet, path, "", "Authorization: Bearer s3cret"); status != 200 {
			t.Fatalf("%s with the token: %d", path, status)
		}
	}
	if status, _ := s.Control(http.MethodGet, "/healthz", ""); status != 200 {
		t.Fatalf("healthz: %d", status)
	}
}
