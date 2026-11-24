package controller_test

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/controller"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
	"github.com/shiv3/gocpp/cp"
	v16h "github.com/shiv3/gocpp/v16/handlers"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v201h "github.com/shiv3/gocpp/v201/handlers"
	v201msg "github.com/shiv3/gocpp/v201/messages"
)

type env struct {
	t   *testing.T
	srv *server.Server
	ext *controller.Ext
}

func start(t *testing.T, opts controller.Options) *env {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	cfg := config.Bind(config.NewBinder(fs, nil))
	if err := fs.Parse(nil); err != nil {
		t.Fatal(err)
	}
	cfg.OCPPAddr, cfg.ControlAddr = "127.0.0.1:0", "127.0.0.1:0"
	cfg.DrainTimeout, cfg.CommandTimeout = time.Second, 3*time.Second
	if opts.NominalVoltage == 0 {
		opts.NominalVoltage = 230
	}
	if opts.ProfileID == 0 {
		opts.ProfileID = 1
	}
	if opts.MeterInterval == 0 {
		opts.MeterInterval = 10 * time.Second
	}
	opts.Enabled = true
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	ext := &controller.Ext{Opts: opts}
	s, err := server.New(cfg, logger, io.Discard, ext)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = s.Shutdown(ctx)
	})
	return &env{t: t, srv: s, ext: ext}
}

func (e *env) dial(id, proto string, register func(*cp.Client) error) *cp.Client {
	e.t.Helper()
	c := cp.NewClient(id, fmt.Sprintf("ws://%s/ocpp/%s", e.srv.OCPPAddr(), id), cp.WithSubProtocols(proto))
	if err := register(c); err != nil {
		e.t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		e.t.Fatal(err)
	}
	e.t.Cleanup(c.Close)
	return c
}

func (e *env) http(method, path, body string) (int, map[string]any) {
	e.t.Helper()
	req, _ := http.NewRequest(method, "http://"+e.srv.ControlAddr().String()+path, strings.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func call(t *testing.T, c *cp.Client, action, payload string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cp.CallRaw(ctx, c, action, []byte(payload))
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return string(resp)
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// recorder logs the commands a fake charge point received.
type recorder struct {
	mu  sync.Mutex
	log []string // "Action {json}"
}

func (r *recorder) add(action string, req any) {
	b, _ := json.Marshal(req)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.log = append(r.log, action+" "+string(b))
}

func (r *recorder) all() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.log...)
}

func (r *recorder) find(prefix string) []string {
	var out []string
	for _, l := range r.all() {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}

func (r *recorder) has(sub string) bool {
	for _, l := range r.all() {
		if strings.Contains(l, sub) {
			return true
		}
	}
	return false
}

// fake16 is an OCPP 1.6 charge point that rejects the Voltage measurand and
// allows three measurands in a list.
type fake16 struct {
	v16h.UnimplementedCPHandler
	*recorder
}

func (f fake16) OnChangeAvailability(_ context.Context, r v16msg.ChangeAvailabilityRequest) (v16msg.ChangeAvailabilityResponse, error) {
	f.add("ChangeAvailability", r)
	return v16msg.ChangeAvailabilityResponse{Status: "Accepted"}, nil
}

func (f fake16) OnGetConfiguration(_ context.Context, r v16msg.GetConfigurationRequest) (v16msg.GetConfigurationResponse, error) {
	f.add("GetConfiguration", r)
	kv := func(k, v string, ro bool) v16msg.ConfigurationKey {
		return v16msg.ConfigurationKey{Key: k, Readonly: ro, Value: &v}
	}
	return v16msg.GetConfigurationResponse{ConfigurationKey: []v16msg.ConfigurationKey{
		kv("ChargeProfileMaxStackLevel", "7", true),
		kv("ChargingScheduleAllowedChargingRateUnit", "Current", true),
		kv("MeterValuesSampledDataMaxLength", "3", true),
		kv("NumberOfConnectors", "2", true),
		kv("SupportedFeatureProfiles", "Core,SmartCharging,RemoteTrigger", true),
		kv("MeterValuesSampledData", "Energy.Active.Import.Register", false),
	}}, nil
}

func (f fake16) OnChangeConfiguration(_ context.Context, r v16msg.ChangeConfigurationRequest) (v16msg.ChangeConfigurationResponse, error) {
	f.add("ChangeConfiguration", r)
	if r.Key == "MeterValuesSampledData" && strings.Contains(r.Value, "Voltage") {
		return v16msg.ChangeConfigurationResponse{Status: "Rejected"}, nil
	}
	return v16msg.ChangeConfigurationResponse{Status: "Accepted"}, nil
}

func (f fake16) OnTriggerMessage(_ context.Context, r v16msg.TriggerMessageRequest) (v16msg.TriggerMessageResponse, error) {
	f.add("TriggerMessage", r)
	return v16msg.TriggerMessageResponse{Status: "Accepted"}, nil
}

func (f fake16) OnSetChargingProfile(_ context.Context, r v16msg.SetChargingProfileRequest) (v16msg.SetChargingProfileResponse, error) {
	f.add("SetChargingProfile", r)
	return v16msg.SetChargingProfileResponse{Status: "Accepted"}, nil
}

func (f fake16) OnClearChargingProfile(_ context.Context, r v16msg.ClearChargingProfileRequest) (v16msg.ClearChargingProfileResponse, error) {
	f.add("ClearChargingProfile", r)
	return v16msg.ClearChargingProfileResponse{Status: "Accepted"}, nil
}

func (f fake16) OnRemoteStartTransaction(_ context.Context, r v16msg.RemoteStartTransactionRequest) (v16msg.RemoteStartTransactionResponse, error) {
	f.add("RemoteStartTransaction", r)
	return v16msg.RemoteStartTransactionResponse{Status: "Accepted"}, nil
}

func (f fake16) register(c *cp.Client) error { return v16h.RegisterCP(c, f) }

const boot16 = `{"chargePointVendor":"Acme","chargePointModel":"W1","firmwareVersion":"1.2"}`

func stationOf(t *testing.T, e *env, id string) map[string]any {
	t.Helper()
	status, body := e.http("GET", "/chargepoints/"+id, "")
	if status != 200 {
		t.Fatalf("GET /chargepoints/%s = %d %v", id, status, body)
	}
	return body["result"].(map[string]any)
}

func TestSetup16(t *testing.T) {
	e := start(t, controller.Options{Setup: true, Measurands: controller.DefaultMeasurands, MeterInterval: 15 * time.Second, PingInterval: 30 * time.Second})
	rec := &recorder{}
	c := e.dial("CP1", "ocpp1.6", fake16{recorder: rec}.register)
	if got := call(t, c, "BootNotification", boot16); !strings.Contains(got, `"Accepted"`) {
		t.Fatalf("boot: %s", got)
	}
	// The final TriggerMessages are the last steps of the sequence.
	eventually(t, "setup to finish", func() bool { return len(rec.find("TriggerMessage")) >= 5 })

	log := rec.all()
	if !strings.HasPrefix(log[0], "ChangeAvailability") || !strings.Contains(log[0], `"connectorId":0`) || !strings.Contains(log[0], "Operative") {
		t.Errorf("first command = %s", log[0])
	}
	if !strings.HasPrefix(log[1], "GetConfiguration") {
		t.Errorf("second command = %s", log[1])
	}
	// One candidate at a time: seven single-measurand probes, Voltage rejected,
	// then the accepted list cut to MeterValuesSampledDataMaxLength (3).
	var values []string
	for _, l := range rec.find("ChangeConfiguration") {
		if strings.Contains(l, "MeterValuesSampledData") {
			values = append(values, l)
		}
	}
	if len(values) != 4 {
		t.Fatalf("MeterValuesSampledData changes = %v", values)
	}
	if !strings.Contains(values[3], `"value":"Power.Active.Import,Energy.Active.Import.Register,Current.Import"`) {
		t.Errorf("final list = %s", values[3])
	}
	if !rec.has(`"key":"MeterValueSampleInterval","value":"15"`) || !rec.has(`"key":"WebSocketPingInterval","value":"30"`) {
		t.Errorf("interval commands missing: %v", log)
	}
	if !rec.has(`TriggerMessage {"connectorId":2,"requestedMessage":"MeterValues"}`) {
		t.Errorf("per-connector trigger missing: %v", log)
	}

	st := stationOf(t, e, "CP1")
	caps := st["capabilities"].(map[string]any)
	if caps["rateUnit"] != "A" || caps["maxStackLevel"] != float64(7) {
		t.Errorf("capabilities = %v", caps)
	}
	if st["boot"].(map[string]any)["vendor"] != "Acme" {
		t.Errorf("boot = %v", st["boot"])
	}
}

func TestSetup16SkipAvailability(t *testing.T) {
	e := start(t, controller.Options{Setup: true, SkipAvailability: true, Measurands: "Power.Active.Import"})
	rec := &recorder{}
	c := e.dial("CP1", "ocpp1.6", fake16{recorder: rec}.register)
	call(t, c, "BootNotification", boot16)
	eventually(t, "setup to finish", func() bool { return len(rec.find("TriggerMessage")) >= 5 })
	if rec.has("ChangeAvailability") {
		t.Errorf("ChangeAvailability sent: %v", rec.all())
	}
}

func TestStateAndLimit16(t *testing.T) {
	e := start(t, controller.Options{})
	rec := &recorder{}
	c := e.dial("CP1", "ocpp1.6", fake16{recorder: rec}.register)
	call(t, c, "BootNotification", boot16)
	call(t, c, "StatusNotification", `{"connectorId":1,"status":"Charging","errorCode":"NoError","timestamp":"2026-01-01T10:00:00Z"}`)
	call(t, c, "MeterValues", `{"connectorId":1,"transactionId":42,"meterValue":[{"timestamp":"2026-01-01T10:00:05Z","sampledValue":[{"value":"1.5","measurand":"Energy.Active.Import.Register","unit":"kWh"},{"value":"7000","measurand":"Power.Active.Import"}]}]}`)

	status, body := e.http("GET", "/chargepoints", "")
	if status != 200 || len(body["result"].([]any)) != 1 {
		t.Fatalf("GET /chargepoints = %d %v", status, body)
	}
	st := stationOf(t, e, "CP1")
	conn := st["connectors"].([]any)[0].(map[string]any)
	if conn["status"] != "Charging" || conn["transactionId"] != "42" {
		t.Fatalf("connector = %v", conn)
	}
	if status, _ := e.http("GET", "/chargepoints/nobody", ""); status != 404 {
		t.Errorf("unknown charge point = %d", status)
	}

	// 6 A on three phases, TxProfile of the recovered transaction.
	status, body = e.http("POST", "/chargepoints/CP1/limit", `{"connectorId":1,"amps":6.04,"phases":3,"purpose":"tx","stackLevel":3}`)
	if status != 200 || body["ok"] != true {
		t.Fatalf("limit = %d %v", status, body)
	}
	got := rec.find("SetChargingProfile")
	if len(got) != 1 {
		t.Fatalf("SetChargingProfile calls = %v", got)
	}
	var req struct {
		ConnectorID int `json:"connectorId"`
		Profile     struct {
			ID       int    `json:"chargingProfileId"`
			Stack    int    `json:"stackLevel"`
			Purpose  string `json:"chargingProfilePurpose"`
			Kind     string `json:"chargingProfileKind"`
			Txn      int    `json:"transactionId"`
			Schedule struct {
				Unit   string `json:"chargingRateUnit"`
				Start  string `json:"startSchedule"`
				Period []struct {
					Start  int     `json:"startPeriod"`
					Limit  float64 `json:"limit"`
					Phases int     `json:"numberPhases"`
				} `json:"chargingSchedulePeriod"`
			} `json:"chargingSchedule"`
		} `json:"csChargingProfiles"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(got[0], "SetChargingProfile ")), &req); err != nil {
		t.Fatal(err)
	}
	p := req.Profile
	if req.ConnectorID != 1 || p.Purpose != "TxProfile" || p.Kind != "Absolute" || p.Txn != 42 || p.Stack != 3 || p.ID != 1 ||
		p.Schedule.Unit != "A" || p.Schedule.Start == "" || len(p.Schedule.Period) != 1 ||
		p.Schedule.Period[0].Limit != 6 || p.Schedule.Period[0].Phases != 3 {
		t.Errorf("profile = %+v", req)
	}
	if limits := stationOf(t, e, "CP1")["limits"].([]any); len(limits) != 1 {
		t.Errorf("limits = %v", limits)
	}

	// tx without a transaction is a conflict; invalid input is a 400.
	if status, _ := e.http("POST", "/chargepoints/CP1/limit", `{"connectorId":2,"amps":10,"purpose":"tx"}`); status != 409 {
		t.Errorf("tx without transaction = %d", status)
	}
	if status, _ := e.http("POST", "/chargepoints/CP1/limit", `{"connectorId":1}`); status != 400 {
		t.Errorf("no limit = %d", status)
	}
	if status, _ := e.http("POST", "/chargepoints/NOPE/limit", `{"connectorId":1,"amps":6}`); status != 404 {
		t.Errorf("unknown cp = %d", status)
	}

	// default purpose, paused, relative.
	if status, _ := e.http("POST", "/chargepoints/CP1/limit", `{"connectorId":1,"amps":0,"relative":true}`); status != 200 {
		t.Fatalf("pause = %d", status)
	}
	last := rec.find("SetChargingProfile")[1]
	if !strings.Contains(last, `"chargingProfilePurpose":"TxDefaultProfile"`) || !strings.Contains(last, `"chargingProfileKind":"Relative"`) || strings.Contains(last, "startSchedule") || !strings.Contains(last, `"limit":0`) {
		t.Errorf("pause profile = %s", last)
	}

	if status, _ := e.http("DELETE", "/chargepoints/CP1/limit?connectorId=1&purpose=default", ""); status != 200 {
		t.Errorf("clear = %d", status)
	}
	if !rec.has(`ClearChargingProfile {"chargingProfilePurpose":"TxDefaultProfile","connectorId":1}`) {
		t.Errorf("clear request: %v", rec.all())
	}
}

func TestRemoteStartPolicy16(t *testing.T) {
	e := start(t, controller.Options{RemoteStartIDTag: "TAG1"})
	rec := &recorder{}
	c := e.dial("CP1", "ocpp1.6", fake16{recorder: rec}.register)
	call(t, c, "BootNotification", boot16)
	call(t, c, "StatusNotification", `{"connectorId":1,"status":"Available","errorCode":"NoError","timestamp":"2026-01-01T10:00:00Z"}`)
	call(t, c, "StatusNotification", `{"connectorId":1,"status":"Preparing","errorCode":"NoError","timestamp":"2026-01-01T10:00:01Z"}`)
	eventually(t, "RemoteStartTransaction", func() bool { return rec.has(`RemoteStartTransaction {"connectorId":1,"idTag":"TAG1"}`) })
	// A stale (older) Preparing report must not start again.
	call(t, c, "StatusNotification", `{"connectorId":2,"status":"Preparing","errorCode":"NoError","timestamp":"2026-01-01T10:00:02Z"}`)
	call(t, c, "StartTransaction", `{"connectorId":2,"idTag":"TAG1","meterStart":0,"timestamp":"2026-01-01T10:00:03Z"}`)
	eventually(t, "second RemoteStartTransaction", func() bool { return len(rec.find("RemoteStartTransaction")) == 2 })
	time.Sleep(50 * time.Millisecond)
	if n := len(rec.find("RemoteStartTransaction")); n != 2 {
		t.Errorf("RemoteStartTransaction calls = %d", n)
	}
}

func TestBootPolicy(t *testing.T) {
	e := start(t, controller.Options{BootPolicy: true, KnownCPs: "KNOWN", BootPendingInter: 17})
	rec := &recorder{}
	unknown := e.dial("OTHER", "ocpp1.6", fake16{recorder: rec}.register)
	got := call(t, unknown, "BootNotification", boot16)
	if !strings.Contains(got, `"status":"Pending"`) || !strings.Contains(got, `"interval":17`) {
		t.Errorf("unknown cp boot = %s", got)
	}
	known := e.dial("KNOWN", "ocpp1.6", fake16{recorder: rec}.register)
	if got := call(t, known, "BootNotification", boot16); !strings.Contains(got, `"status":"Accepted"`) {
		t.Errorf("known cp boot = %s", got)
	}
}

func TestFaults(t *testing.T) {
	e := start(t, controller.Options{Faults: true})
	c := e.dial("CP1", "ocpp1.6", fake16{recorder: &recorder{}}.register)

	if status, _ := e.http("PUT", "/faults", `{"rules":[{"kind":"duplicate","action":"Heartbeat"}]}`); status != 400 {
		t.Errorf("duplicate rule = %d", status)
	}
	if status, _ := e.http("PUT", "/faults", `{"rules":[{"kind":"latency","action":"Heartbeat","delay":"nope"}]}`); status != 400 {
		t.Errorf("bad delay = %d", status)
	}

	status, _ := e.http("PUT", "/faults", `{"rules":[
		{"kind":"error","action":"Authorize","errorCode":"InternalError","description":"injected","count":1},
		{"kind":"latency","action":"Heartbeat","delay":"300ms","cpId":"CP1"},
		{"kind":"ignore","action":"DataTransfer"}]}`)
	if status != 200 {
		t.Fatalf("PUT /faults = %d", status)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cp.CallRaw(ctx, c, "Authorize", []byte(`{"idTag":"A"}`)); err == nil || !strings.Contains(err.Error(), "InternalError") {
		t.Errorf("Authorize error = %v", err)
	}
	// count 1: the next Authorize is answered normally.
	if got := call(t, c, "Authorize", `{"idTag":"A"}`); !strings.Contains(got, "Accepted") {
		t.Errorf("Authorize after the rule expired = %s", got)
	}
	begin := time.Now()
	call(t, c, "Heartbeat", `{}`)
	if d := time.Since(begin); d < 300*time.Millisecond {
		t.Errorf("Heartbeat answered after %v", d)
	}
	short, cancel2 := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel2()
	if _, err := cp.CallRaw(short, c, "DataTransfer", []byte(`{"vendorId":"x"}`)); err == nil {
		t.Error("ignored DataTransfer was answered")
	}

	if status, _ := e.http("DELETE", "/faults", ""); status != 200 {
		t.Errorf("DELETE /faults = %d", status)
	}
	if got := call(t, c, "Heartbeat", `{}`); !strings.Contains(got, "currentTime") {
		t.Errorf("Heartbeat = %s", got)
	}
}

type fake201 struct {
	v201h.UnimplementedCPHandler
	*recorder
}

func (f fake201) OnChangeAvailability(_ context.Context, r v201msg.ChangeAvailabilityRequest) (v201msg.ChangeAvailabilityResponse, error) {
	f.add("ChangeAvailability", r)
	return v201msg.ChangeAvailabilityResponse{Status: "Accepted"}, nil
}

func (f fake201) OnGetVariables(_ context.Context, r v201msg.GetVariablesRequest) (v201msg.GetVariablesResponse, error) {
	f.add("GetVariables", r)
	out := v201msg.GetVariablesResponse{}
	for _, d := range r.GetVariableData {
		res := v201msg.GetVariableResultType{AttributeStatus: "Accepted", Component: d.Component, Variable: d.Variable}
		var val string
		switch d.Variable.Name {
		case "RateUnit":
			val = "A,W"
		case "ProfileStackLevel":
			val = "5"
		default:
			res.AttributeStatus = "UnknownVariable"
		}
		if res.AttributeStatus == "Accepted" {
			res.AttributeValue = &val
		}
		out.GetVariableResult = append(out.GetVariableResult, res)
	}
	return out, nil
}

func (f fake201) OnSetVariables(_ context.Context, r v201msg.SetVariablesRequest) (v201msg.SetVariablesResponse, error) {
	f.add("SetVariables", r)
	out := v201msg.SetVariablesResponse{}
	for _, d := range r.SetVariableData {
		st := "Accepted"
		if strings.Contains(d.AttributeValue, "Voltage") {
			st = "Rejected"
		}
		out.SetVariableResult = append(out.SetVariableResult, v201msg.SetVariableResultType{AttributeStatus: st, Component: d.Component, Variable: d.Variable})
	}
	return out, nil
}

func (f fake201) OnTriggerMessage(_ context.Context, r v201msg.TriggerMessageRequest) (v201msg.TriggerMessageResponse, error) {
	f.add("TriggerMessage", r)
	return v201msg.TriggerMessageResponse{Status: "Accepted"}, nil
}

func (f fake201) OnSetChargingProfile(_ context.Context, r v201msg.SetChargingProfileRequest) (v201msg.SetChargingProfileResponse, error) {
	f.add("SetChargingProfile", r)
	return v201msg.SetChargingProfileResponse{Status: "Accepted"}, nil
}

func (f fake201) OnRequestStartTransaction(_ context.Context, r v201msg.RequestStartTransactionRequest) (v201msg.RequestStartTransactionResponse, error) {
	f.add("RequestStartTransaction", r)
	return v201msg.RequestStartTransactionResponse{Status: "Accepted"}, nil
}

func (f fake201) register(c *cp.Client) error { return v201h.RegisterCP(c, f) }

const boot201 = `{"reason":"PowerUp","chargingStation":{"model":"W2","vendorName":"Acme"}}`

func TestSetupStateLimitRemoteStart201(t *testing.T) {
	e := start(t, controller.Options{Setup: true, RemoteStartIDTag: "TAG2", Measurands: "Power.Active.Import,Voltage,Current.Import", MeterInterval: 20 * time.Second, PingInterval: 45 * time.Second})
	rec := &recorder{}
	c := e.dial("CP2", "ocpp2.0.1", fake201{recorder: rec}.register)
	if got := call(t, c, "BootNotification", boot201); !strings.Contains(got, "Accepted") {
		t.Fatalf("boot: %s", got)
	}
	eventually(t, "setup to finish", func() bool { return len(rec.find("TriggerMessage")) >= 2 })
	if !rec.has(`"attributeValue":"Power.Active.Import,Current.Import"`) || !rec.has(`"attributeValue":"20"`) || !rec.has(`"attributeValue":"45"`) {
		t.Errorf("setup commands: %v", rec.all())
	}
	st := stationOf(t, e, "CP2")
	if st["capabilities"].(map[string]any)["maxStackLevel"] != float64(5) {
		t.Errorf("capabilities = %v", st["capabilities"])
	}

	call(t, c, "StatusNotification", `{"timestamp":"2026-01-01T10:00:00Z","connectorStatus":"Occupied","evseId":1,"connectorId":1}`)
	eventually(t, "RequestStartTransaction", func() bool { return rec.has(`"idToken":{"idToken":"TAG2","type":"Central"}`) })
	call(t, c, "TransactionEvent", `{"eventType":"Started","timestamp":"2026-01-01T10:00:02Z","triggerReason":"Authorized","seqNo":0,"transactionInfo":{"transactionId":"tx-1"},"evse":{"id":1,"connectorId":1},"meterValue":[{"timestamp":"2026-01-01T10:00:02Z","sampledValue":[{"value":2,"measurand":"Energy.Active.Import.Register","unitOfMeasure":{"unit":"kWh"}}]}]}`)

	status, body := e.http("POST", "/chargepoints/CP2/limit", `{"connectorId":1,"watts":6900,"phases":3,"purpose":"tx"}`)
	if status != 200 {
		t.Fatalf("limit = %d %v", status, body)
	}
	got := rec.find("SetChargingProfile")
	if len(got) != 1 {
		t.Fatalf("SetChargingProfile = %v", got)
	}
	// 6900 W / (230 V * 3) = 10 A in the charge point's unit (A).
	for _, want := range []string{`"evseId":1`, `"chargingProfilePurpose":"TxProfile"`, `"transactionId":"tx-1"`, `"chargingRateUnit":"A"`, `"limit":10`, `"numberPhases":3`, `"stackLevel":5`} {
		if !strings.Contains(got[0], want) {
			t.Errorf("profile lacks %s: %s", want, got[0])
		}
	}
	conn := stationOf(t, e, "CP2")["connectors"].([]any)[0].(map[string]any)
	if conn["transactionId"] != "tx-1" {
		t.Errorf("connector = %v", conn)
	}
	m := conn["measurements"].([]any)[0].(map[string]any)
	if m["value"] != float64(2000) || m["unit"] != "Wh" {
		t.Errorf("measurement = %v", m)
	}
}
