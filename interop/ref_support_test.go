//go:build interop

package interop

// Reference-peer harness. The peers are the independent OCPP implementations
// that github.com/otfabric/ocpp-interop (MIT) packages as containers behind one
// command-line contract (its docs/CONTAINER_CONTRACT.md). This file is a
// from-scratch reimplementation of the small part of that contract gocpp needs:
// start a peer as a station or a CSMS, make it send a CALL, read the fixture it
// answers from. Nothing of otfabric's code is copied; the image references are
// the only third-party data (the digests were qualified by otfabric/go-ocpp's
// own interop suite, also MIT).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type peerSpec struct {
	name     string
	image    string
	upstream string
}

// peerSpecs are the pinned reference images (ocpp-interop v0.10.0, contract 0.8).
// Override one with OCPP_INTEROP_<NAME>_IMAGE (for example a locally built :dev).
var peerSpecs = []peerSpec{
	{"mobilityhouse", "ghcr.io/otfabric/ocpp-interop-mobilityhouse@sha256:347b228ffa4b88f6bad36c8c170e3f13b95489f29455a2c28ca8c1e760b26b97", "mobilityhouse/ocpp 2.1.0 (Python)"},
	{"chargetime", "ghcr.io/otfabric/ocpp-interop-chargetime@sha256:a455edea1b1f6b7cabcbe60dce7caead9d44aef63ef4f1c7c1b73077cf3d10e9", "ChargeTimeEU/Java-OCA-OCPP v2.0 (Java)"},
	{"lorenzodonini", "ghcr.io/otfabric/ocpp-interop-lorenzodonini@sha256:902cea48e055575420eb3537f4ad823adb067664896158f8d5d6f71a23b62afc", "lorenzodonini/ocpp-go v0.19.0 (Go)"},
	{"everest", "ghcr.io/otfabric/ocpp-interop-everest@sha256:e56ec2d4033e7ede137c8801861ec7f3c84eb7f83f8ccb5e3cc29324005b9f7c", "EVerest libocpp, everest-core 2026.02.2 (C++), station only"},
	{"citrineos", "ghcr.io/otfabric/ocpp-interop-citrineos@sha256:c0da2ffe666b0f1926d44ff35fc06036503e4f76d6b817d319a0c97dd7dd32e5", "CitrineOS core v2.1.0 (TypeScript), CSMS only"},
	{"cbsigma", "ghcr.io/otfabric/ocpp-interop-cbsigma@sha256:57b20270aec341de671cfda609b22e46b71914870b22b12357aada321fc45297", "cB-Sigma/ocpp v0.2.0 (Go)"},
	{"ocppkit", "ghcr.io/otfabric/ocpp-interop-ocppkit@sha256:1c51e474386fdfc7a47c00edbc91243f4cc6ef0a9d0c4d9ebb3b0f8dc99f5ef7", "hupe1980/ocpp-kit 0.3.0 (Rust)"},
	{"seanlee", "ghcr.io/otfabric/ocpp-interop-seanlee@sha256:bab883c4f4f43592c0d78f2b7ab1a2195c8d16a23aacb9afa4fe3b4f388d7e3a", "seanlee0923/ocpp v0.3.0 (Go)"},
}

type versionSpec struct{ ocpp, sub string }

var versionSpecs = []versionSpec{
	{"1.6", "ocpp1.6"},
	{"2.0.1", "ocpp2.0.1"},
	{"2.1", "ocpp2.1"},
}

func (p peerSpec) imageRef() string {
	if v := os.Getenv("OCPP_INTEROP_" + strings.ToUpper(p.name) + "_IMAGE"); v != "" {
		return v
	}
	return p.image
}

// selectedPeers and selectedVersions honour INTEROP_PEERS / INTEROP_VERSIONS
// (comma separated).
func selected(env string, all []string) []string {
	v := strings.TrimSpace(os.Getenv(env))
	if v == "" {
		return all
	}
	var out []string
	for _, s := range strings.Split(v, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func selectedPeers() []peerSpec {
	var names []string
	for _, p := range peerSpecs {
		names = append(names, p.name)
	}
	want := map[string]bool{}
	for _, n := range selected("INTEROP_PEERS", names) {
		want[n] = true
	}
	var out []peerSpec
	for _, p := range peerSpecs {
		if want[p.name] {
			out = append(out, p)
		}
	}
	return out
}

func selectedVersions() []versionSpec {
	want := map[string]bool{}
	for _, n := range selected("INTEROP_VERSIONS", []string{"1.6", "2.0.1", "2.1"}) {
		want[n] = true
	}
	var out []versionSpec
	for _, v := range versionSpecs {
		if want[v.ocpp] {
			out = append(out, v)
		}
	}
	return out
}

// ---- container runner ------------------------------------------------------

func dockerBin() string {
	if v := os.Getenv("INTEROP_DOCKER"); v != "" {
		return v
	}
	return "docker"
}

func docker(timeout time.Duration, args ...string) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, dockerBin(), args...)
	var so, se bytes.Buffer
	cmd.Stdout, cmd.Stderr = &so, &se
	err := cmd.Run()
	return so.String(), se.String(), err
}

var containerSeq atomic.Int64

// freePort returns a TCP port that was free a moment ago on the loopback.
func freePort(t testing.TB) int {
	t.Helper()
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

// startContainer runs image detached on the host network with the given
// docker flags and contract arguments, waits for its readiness file, and
// removes it when the test ends.
func startContainer(t *testing.T, image string, dockerArgs []string, args ...string) string {
	t.Helper()
	name := fmt.Sprintf("gocpp-interop-%d-%d", os.Getpid(), containerSeq.Add(1))
	full := append([]string{"run", "-d", "--network", "host", "--name", name}, dockerArgs...)
	full = append(full, image)
	full = append(full, args...)
	if _, se, err := docker(120*time.Second, full...); err != nil {
		t.Fatalf("docker run: %v: %s", err, se)
	}
	t.Cleanup(func() { _, _, _ = docker(60*time.Second, "rm", "-f", name) })
	deadline := time.Now().Add(150 * time.Second)
	for time.Now().Before(deadline) {
		if _, _, err := docker(20*time.Second, "exec", name, "test", "-f", "/run/ocpp-interop/ready"); err == nil {
			return name
		}
		if out, _, _ := docker(20*time.Second, "ps", "-q", "--filter", "name="+name); strings.TrimSpace(out) == "" {
			lg, _, _ := docker(20*time.Second, "logs", name)
			if code, _, _ := docker(20*time.Second, "inspect", "-f", "{{.State.ExitCode}}", name); strings.TrimSpace(code) == "5" {
				// Contract exit code 5: the adapter does not support what was asked.
				t.Skipf("not supported by the peer (exit 5): %s", tail(strings.TrimSpace(lg), 300))
			}
			t.Fatalf("container %s exited before it was ready:\n%s", name, tail(lg, 3000))
		}
		time.Sleep(500 * time.Millisecond)
	}
	lg, _, _ := docker(20*time.Second, "logs", name)
	t.Fatalf("container %s not ready in time:\n%s", name, tail(lg, 3000))
	return ""
}

func tail(s string, n int) string {
	if len(s) > n {
		return "..." + s[len(s)-n:]
	}
	return s
}

// containerEvents returns the JSON-lines events the contract writes to stdout.
func containerEvents(name string) []map[string]any {
	so, se, _ := docker(30*time.Second, "logs", name)
	var out []map[string]any
	for _, line := range strings.Split(so+"\n"+se, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["event"] != nil {
			out = append(out, m)
		}
	}
	return out
}

type refErr struct {
	Code        string `json:"code"`
	Description string `json:"description"`
	Message     string `json:"message"`
}

type refResult struct {
	OK        bool            `json:"ok"`
	Request   json.RawMessage `json:"request"`
	Response  json.RawMessage `json:"response"`
	CallError *refErr         `json:"callError"`
	Error     *refErr         `json:"error"`
	Raw       string          `json:"-"`
}

// refCall makes the running container send one CALL (contract: `call`).
func refCall(t *testing.T, name, station, action string, payload json.RawMessage) refResult {
	t.Helper()
	args := []string{"exec", name, "ocpp-interop", "call"}
	if station != "" {
		args = append(args, "--station", station)
	}
	args = append(args, action, "--payload", string(payload), "--timeout-ms", "15000")
	so, se, _ := docker(40*time.Second, args...)
	lines := strings.Split(strings.TrimSpace(so), "\n")
	last := lines[len(lines)-1]
	var r refResult
	r.Raw = last
	if err := json.Unmarshal([]byte(last), &r); err != nil {
		t.Fatalf("call %s: no result document (stdout %q, stderr %q)", action, so, tail(se, 500))
	}
	return r
}

// ---- capabilities and fixtures --------------------------------------------

type roleCaps struct {
	Supported        bool     `json:"supported"`
	SecurityProfiles []int    `json:"securityProfiles"`
	Sends            []string `json:"sends"`
	Handles          []string `json:"handles"`
}

type featureCaps struct {
	SecurityProfiles []int `json:"securityProfiles"`
	Compression      bool  `json:"compression"`
	Send             bool  `json:"send"`
	FixtureAnswers   *bool `json:"fixtureAnswers"`
}

type versionCaps struct {
	Client   roleCaps    `json:"client"`
	Server   roleCaps    `json:"server"`
	Features featureCaps `json:"features"`
}

type capabilities struct {
	Adapter  string                   `json:"adapter"`
	Version  string                   `json:"adapterVersion"`
	Upstream struct{ Version string } `json:"upstream"`
	Versions map[string]versionCaps   `json:"versions"`
}

func (c versionCaps) fixtureAnswers() bool {
	return c.Features.FixtureAnswers == nil || *c.Features.FixtureAnswers
}

func (r roleCaps) sends(a string) bool   { return contains(r.Sends, a) }
func (r roleCaps) handles(a string) bool { return contains(r.Handles, a) }

func contains(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

var (
	cacheMu   sync.Mutex
	capsCache = map[string]*capabilities{}
	fxCache   = map[string]*fixture{}
)

func loadCaps(t *testing.T, p peerSpec) *capabilities {
	t.Helper()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	if c, ok := capsCache[p.name]; ok {
		return c
	}
	so, se, err := docker(300*time.Second, "run", "--rm", p.imageRef(), "print-capabilities")
	if err != nil {
		t.Fatalf("print-capabilities %s: %v: %s", p.name, err, tail(se, 800))
	}
	var c capabilities
	if err := json.Unmarshal([]byte(strings.TrimSpace(lastLine(so))), &c); err != nil {
		t.Fatalf("capabilities of %s: %v", p.name, err)
	}
	capsCache[p.name] = &c
	return &c
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	return lines[len(lines)-1]
}

type answer struct {
	Payload   json.RawMessage `json:"payload"`
	Derived   string          `json:"derived"`
	CallError *refErr         `json:"callError"`
}

type sample struct {
	Action  string          `json:"action"`
	From    string          `json:"from"`
	Payload json.RawMessage `json:"payload"`
}

type fixture struct {
	Stations []struct {
		ID       string `json:"id"`
		Password string `json:"password"`
	} `json:"stations"`
	IDTokens []struct {
		IDToken string `json:"idToken"`
		Type    string `json:"type"`
		Status  string `json:"status"`
	} `json:"idTokens"`
	Configuration []struct {
		Key      string `json:"key"`
		Value    string `json:"value"`
		Readonly bool   `json:"readonly"`
	} `json:"configuration"`
	Variables []struct {
		Component  struct{ Name, Instance string } `json:"component"`
		Variable   struct{ Name, Instance string } `json:"variable"`
		Value      string                          `json:"value"`
		Mutability string                          `json:"mutability"`
	} `json:"variables"`
	Answers struct {
		CSMS    map[string]answer `json:"csms"`
		Station map[string]answer `json:"station"`
	} `json:"answers"`
	Samples map[string]sample `json:"samples"`
}

func (f *fixture) sampleNames() []string {
	var n []string
	for k := range f.Samples {
		n = append(n, k)
	}
	sort.Strings(n)
	return n
}

// loadFixture reads the baseline fixture from the peer's own image
// (print-fixture), so expectations cannot drift from what the image serves.
func loadFixture(t *testing.T, p peerSpec, v versionSpec) *fixture {
	t.Helper()
	cacheMu.Lock()
	defer cacheMu.Unlock()
	key := p.name + "@" + v.ocpp
	if f, ok := fxCache[key]; ok {
		return f
	}
	so, se, err := docker(300*time.Second, "run", "--rm", p.imageRef(), "print-fixture", "--version", v.ocpp)
	if err != nil {
		t.Fatalf("print-fixture %s %s: %v: %s", p.name, v.ocpp, err, tail(se, 800))
	}
	var f fixture
	if err := json.Unmarshal([]byte(so), &f); err != nil {
		t.Fatalf("fixture of %s %s: %v", p.name, v.ocpp, err)
	}
	fxCache[key] = &f
	return &f
}

// ---- JSON comparison -------------------------------------------------------

var nowPlaceholder = "${now}"

// subset reports whether every value in want is present in got: objects by key
// (got may have more), arrays element by element, numbers numerically, strings
// equal or the same RFC 3339 instant; the string "${now}" matches any string.
func subset(want, got any, path string) error {
	switch w := want.(type) {
	case map[string]any:
		g, ok := got.(map[string]any)
		if !ok {
			return fmt.Errorf("%s: want object, got %T", path, got)
		}
		for k, wv := range w {
			gv, ok := g[k]
			if !ok {
				if wv == nil {
					continue
				}
				return fmt.Errorf("%s.%s: missing", path, k)
			}
			if err := subset(wv, gv, path+"."+k); err != nil {
				return err
			}
		}
		return nil
	case []any:
		g, ok := got.([]any)
		if !ok {
			return fmt.Errorf("%s: want array, got %T", path, got)
		}
		if len(g) != len(w) {
			return fmt.Errorf("%s: want %d elements, got %d", path, len(w), len(g))
		}
		for i := range w {
			if err := subset(w[i], g[i], fmt.Sprintf("%s[%d]", path, i)); err != nil {
				return err
			}
		}
		return nil
	case string:
		gs, ok := got.(string)
		if !ok {
			return fmt.Errorf("%s: want string %q, got %v", path, w, got)
		}
		if w == nowPlaceholder || w == gs {
			return nil
		}
		wt, e1 := time.Parse(time.RFC3339Nano, w)
		gt, e2 := time.Parse(time.RFC3339Nano, gs)
		if e1 == nil && e2 == nil && wt.Equal(gt) {
			return nil
		}
		return fmt.Errorf("%s: want %q, got %q", path, w, gs)
	case float64:
		gf, ok := got.(float64)
		if !ok || gf != w {
			return fmt.Errorf("%s: want %v, got %v", path, w, got)
		}
		return nil
	default:
		if fmt.Sprint(want) != fmt.Sprint(got) {
			return fmt.Errorf("%s: want %v, got %v", path, want, got)
		}
		return nil
	}
}

func jsonSubset(want, got []byte) error {
	var w, g any
	if err := json.Unmarshal(want, &w); err != nil {
		return fmt.Errorf("expected payload: %v", err)
	}
	if err := json.Unmarshal(got, &g); err != nil {
		return fmt.Errorf("actual payload %q: %v", got, err)
	}
	return subset(w, g, "$")
}

// ---- fixture responder -----------------------------------------------------

// responder answers CALLs the way the fixture defines (docs/FIXTURES.md of
// ocpp-interop): fixed payloads, "${now}", and the derived answers idTokens,
// configuration and variables.
type responder struct {
	f   *fixture
	ver string
	mu  sync.Mutex
	set map[string]string // SetVariables state, component/variable -> value
	// keepNow leaves "${now}" in answers (an expectation, matched by any string).
	keepNow bool
	// muted actions are answered NotImplemented, to check that behaviour.
	muted map[string]bool
}

// expectation returns a responder whose answers keep the "${now}" placeholder.
func newExpectation(f *fixture, ver string) *responder {
	r := newResponder(f, ver)
	r.keepNow = true
	return r
}

func (r *responder) mute(action string, on bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.muted == nil {
		r.muted = map[string]bool{}
	}
	r.muted[action] = on
}

func newResponder(f *fixture, ver string) *responder {
	return &responder{f: f, ver: ver, set: map[string]string{}}
}

type callErr struct{ code, desc string }

func (e *callErr) Error() string { return e.code + ": " + e.desc }

func (r *responder) table(side string) map[string]answer {
	if side == "csms" {
		return r.f.Answers.CSMS
	}
	return r.f.Answers.Station
}

func stripNow(raw []byte) []byte {
	return bytes.ReplaceAll(raw, []byte(`"${now}"`), []byte(`"`+time.Now().UTC().Format(time.RFC3339)+`"`))
}

// answer returns the response payload the fixture defines for action, answered
// by side ("csms" or "station"), or a *callErr.
func (r *responder) answer(side, action string, req []byte) ([]byte, error) {
	r.mu.Lock()
	muted := r.muted[action]
	r.mu.Unlock()
	a, ok := r.table(side)[action]
	if !ok || muted {
		return nil, &callErr{"NotImplemented", "no fixture answer for " + action}
	}
	if a.CallError != nil {
		return nil, &callErr{a.CallError.Code, a.CallError.Description}
	}
	base := map[string]any{}
	if len(a.Payload) > 0 {
		pl := []byte(a.Payload)
		if !r.keepNow {
			pl = stripNow(pl)
		}
		if err := json.Unmarshal(pl, &base); err != nil {
			return nil, err
		}
	}
	var in map[string]any
	_ = json.Unmarshal(req, &in)
	switch a.Derived {
	case "":
	case "idTokens":
		st := r.tokenStatus(in)
		if r.ver == "1.6" {
			base["idTagInfo"] = map[string]any{"status": st}
		} else {
			base["idTokenInfo"] = map[string]any{"status": st}
		}
	case "configuration":
		r.deriveConfiguration(in, base)
	case "variables":
		r.deriveVariables(action, in, base)
	default:
		return nil, fmt.Errorf("unknown derived answer %q", a.Derived)
	}
	return json.Marshal(base)
}

func (r *responder) tokenStatus(in map[string]any) string {
	if r.ver == "1.6" {
		tag, _ := in["idTag"].(string)
		for _, t := range r.f.IDTokens {
			if t.IDToken == tag {
				return t.Status
			}
		}
		return "Invalid"
	}
	tok, _ := in["idToken"].(map[string]any)
	id, _ := tok["idToken"].(string)
	typ, _ := tok["type"].(string)
	for _, t := range r.f.IDTokens {
		if t.IDToken == id && t.Type == typ {
			return t.Status
		}
	}
	return "Invalid"
}

func (r *responder) deriveConfiguration(in, out map[string]any) {
	var want []string
	if ks, ok := in["key"].([]any); ok {
		for _, k := range ks {
			want = append(want, fmt.Sprint(k))
		}
	}
	var cfg []map[string]any
	var unknown []string
	if len(want) == 0 {
		for _, c := range r.f.Configuration {
			cfg = append(cfg, map[string]any{"key": c.Key, "readonly": c.Readonly, "value": c.Value})
		}
	}
	for _, w := range want {
		found := false
		for _, c := range r.f.Configuration {
			if strings.EqualFold(c.Key, w) {
				cfg = append(cfg, map[string]any{"key": c.Key, "readonly": c.Readonly, "value": c.Value})
				found = true
				break
			}
		}
		if !found {
			unknown = append(unknown, w)
		}
	}
	if len(cfg) > 0 {
		out["configurationKey"] = cfg
	}
	if len(unknown) > 0 {
		out["unknownKey"] = unknown
	}
}

func (r *responder) deriveVariables(action string, in, out map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	listKey, resKey := "getVariableData", "getVariableResult"
	if action == "SetVariables" {
		listKey, resKey = "setVariableData", "setVariableResult"
	}
	items, _ := in[listKey].([]any)
	var results []any
	for _, it := range items {
		m, _ := it.(map[string]any)
		comp, _ := m["component"].(map[string]any)
		vr, _ := m["variable"].(map[string]any)
		res := map[string]any{"component": comp, "variable": vr}
		if at, ok := m["attributeType"]; ok {
			res["attributeType"] = at
		}
		cn, _ := comp["name"].(string)
		ci, _ := comp["instance"].(string)
		vn, _ := vr["name"].(string)
		vi, _ := vr["instance"].(string)
		status := "UnknownComponent"
		var value string
		haveComp := false
		for _, v := range r.f.Variables {
			if !strings.EqualFold(v.Component.Name, cn) || !strings.EqualFold(v.Component.Instance, ci) {
				continue
			}
			haveComp = true
			if !strings.EqualFold(v.Variable.Name, vn) || !strings.EqualFold(v.Variable.Instance, vi) {
				continue
			}
			key := strings.ToLower(cn + "/" + ci + "/" + vn + "/" + vi)
			value = v.Value
			if s, ok := r.set[key]; ok {
				value = s
			}
			status = "Accepted"
			if at, _ := m["attributeType"].(string); at != "" && at != "Actual" {
				status = "NotSupportedAttributeType"
			} else if action == "GetVariables" && v.Mutability == "WriteOnly" {
				status = "Rejected"
			} else if action == "SetVariables" && v.Mutability == "ReadOnly" {
				status = "Rejected"
			} else if action == "SetVariables" {
				r.set[key], _ = m["attributeValue"].(string)
			}
			break
		}
		if status == "UnknownComponent" && haveComp {
			status = "UnknownVariable"
		}
		res["attributeStatus"] = status
		if action == "GetVariables" && status == "Accepted" {
			res["attributeValue"] = value
		}
		results = append(results, res)
	}
	out[resKey] = results
}

// ---- known issues ----------------------------------------------------------

// knownIssue maps a failing scenario to a recorded cause, so the matrix can
// tell a gocpp bug from a peer bug or a harness limitation. Failures that match
// are reported as skips whose reason starts with "KNOWN" and the class.
type knownIssue struct {
	class string // "peer", "harness", "gocpp"
	re    *regexp.Regexp
	why   string
}

func mustKnown(class, re, why string) knownIssue {
	return knownIssue{class, regexp.MustCompile(re), why}
}

// knownIssues are the failures this suite has analysed. Peer defects that
// otfabric/ocpp-interop also records (tests/known-bugs.tsv) cite its evidence.
var knownIssues = []knownIssue{
	mustKnown("peer", `^TestInterop_GocppCSMS/mobilityhouse/1\.6/peer-to-gocpp/securityEventMinimal$`,
		"mobilityhouse/ocpp 2.1.0: v16.call.SecurityEventNotification declares tech_info without a default, so the station cannot omit the optional techInfo"),
	mustKnown("peer", `^TestInterop_Gocpp(CSMS|CP)/mobilityhouse/2\.1/unknown-action/`,
		"mobilityhouse/ocpp 2.1.0 never answers a 2.1 CALL it has no handler for (neither NotImplemented nor NotSupported); fixed on its master, unreleased"),
	mustKnown("peer", `^TestInterop_GocppCSMS/mobilityhouse/2\.1/unanswered-action/`,
		"mobilityhouse/ocpp 2.1.0 does not report a CALLERROR for an unanswered 2.1 action (see above)"),
	mustKnown("peer", `^TestInterop_Gocpp(CSMS|CP)/lorenzodonini/2\.0\.1/(gocpp-to-peer|peer-to-gocpp)/getCompositeSchedule$`,
		"lorenzodonini/ocpp-go v0.19.0: smartcharging.GetCompositeScheduleResponse does not have the OCA structure (top-level evseId, schedule of another type)"),
	mustKnown("peer", `^TestInterop_GocppCSMS/lorenzodonini/2\.0\.1/peer-to-gocpp/notifyReport$`,
		"lorenzodonini/ocpp-go v0.19.0: NotifyReportRequest declares Tbc as a bool with omitempty, so an explicit tbc:false is dropped from the wire (false is the default)"),
	mustKnown("peer", `^TestInterop_GocppCSMS/everest/2\.1/peer-to-gocpp/reportDERControl$`,
		"EVerest libocpp (everest-core 2026.02.2): ocpp::v2::MessageType has no ReportDERControl, so libocpp cannot send it"),
	mustKnown("peer", `^TestInterop_Gocpp(CSMS|CP)_Security/chargetime/2\.0\.1/compression$`,
		"ChargeTimeEU Java-OCA-OCPP v2.0 / Java-WebSocket 1.6.0: permessage-deflate is announced without context takeover but the context is kept (corrupt frames)"),
}

func known(id string) *knownIssue {
	for i := range knownIssues {
		if knownIssues[i].re.MatchString(id) {
			return &knownIssues[i]
		}
	}
	return nil
}

// check runs fn as a subtest named by the scenario; a failure that matches a
// known issue is downgraded to a skip with the classification.
func check(t *testing.T, id string, fn func(t *testing.T)) {
	t.Helper()
	t.Run(id, func(t *testing.T) {
		fn(t)
	})
}

// failKnown fails the test, or skips it when the scenario is a recorded issue.
func failKnown(t *testing.T, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	if k := known(t.Name()); k != nil {
		t.Skipf("KNOWN[%s]: %s | observed: %s", k.class, k.why, msg)
	}
	t.Fatal(msg)
}
