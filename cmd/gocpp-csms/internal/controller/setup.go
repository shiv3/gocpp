package controller

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/station"
	"github.com/shiv3/gocpp/core/ocppj"
)

// keyedConfig is the explicit key list requested when reading the whole
// configuration fails.
var keyedConfig = []string{
	"ChargeProfileMaxStackLevel", "ChargingScheduleAllowedChargingRateUnit",
	"ConnectorSwitch3to1PhaseSupported", "ACPhaseSwitchingSupported",
	"MaxChargingProfilesInstalled", "MeterValuesSampledData",
	"MeterValuesSampledDataMaxLength", "NumberOfConnectors", "SupportedFeatureProfiles",
}

// Variables read in 2.x setup, as component/variable.
var variables2 = [][2]string{
	{"SampledDataCtrlr", "TxUpdatedMeasurands"},
	{"SampledDataCtrlr", "TxUpdatedInterval"},
	{"OCPPCommCtrlr", "WebSocketPingInterval"},
	{"SmartChargingCtrlr", "RateUnit"},
	{"SmartChargingCtrlr", "ProfileStackLevel"},
	{"SmartChargingCtrlr", "ACPhaseSwitchingSupported"},
	{"SmartChargingCtrlr", "EntriesChargingProfiles"},
}

// candidates returns the measurands to try: the default list, plus the
// entries of spec, minus the "-name" ones.
func candidates(spec string) []string {
	var out []string
	remove := map[string]bool{}
	var add []string
	for _, m := range strings.Split(spec, ",") {
		m = strings.TrimSpace(m)
		switch {
		case m == "":
		case strings.HasPrefix(m, "-"):
			remove[m[1:]] = true
		default:
			add = append(add, m)
		}
	}
	if len(add) == 0 {
		add = strings.Split(DefaultMeasurands, ",")
	}
	for _, m := range add {
		if !remove[m] {
			out = append(out, m)
		}
	}
	return out
}

// runSetup configures a charge point after an Accepted boot. It never aborts
// on a failed step: the outcome of each is in the setup report of the station.
func (e *Ext) runSetup(ctx context.Context, cp string, v ocppj.Version) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	e.reg.ResetSetup(cp)
	if v == ocppj.V16 {
		e.setup16(ctx, cp)
	} else {
		e.setup2(ctx, cp)
	}
}

// step sends a command and records the outcome; ok is true on an Accepted (or
// otherwise successful) answer, status is the answer's "status" member.
func (e *Ext) step(ctx context.Context, cp, action, detail string, payload any) (resp json.RawMessage, status string, ok bool) {
	resp, err := e.send(ctx, cp, action, payload)
	if err != nil {
		e.reg.AddStep(cp, action, detail, "error: "+err.Error())
		return nil, "", false
	}
	var ans struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(resp, &ans)
	e.reg.AddStep(cp, action, detail, orDefault(ans.Status, "ok"))
	return resp, ans.Status, true
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

func (e *Ext) setup16(ctx context.Context, cp string) {
	o := &e.Opts
	if !o.SkipAvailability {
		e.step(ctx, cp, "ChangeAvailability", "connector 0 Operative", map[string]any{"connectorId": 0, "type": "Operative"})
	}

	// Read the configuration; fall back to an explicit key list.
	resp, _, ok := e.step(ctx, cp, "GetConfiguration", "all keys", map[string]any{})
	if !ok {
		resp, _, ok = e.step(ctx, cp, "GetConfiguration", "key list", map[string]any{"key": keyedConfig})
	}
	cfg := map[string]string{}
	readonly := map[string]bool{}
	if ok {
		var r struct {
			ConfigurationKey []struct {
				Key      string  `json:"key"`
				Readonly bool    `json:"readonly"`
				Value    *string `json:"value"`
			} `json:"configurationKey"`
		}
		if json.Unmarshal(resp, &r) == nil {
			for _, k := range r.ConfigurationKey {
				val := ""
				if k.Value != nil {
					val = *k.Value
				}
				cfg[k.Key] = val
				readonly[k.Key] = k.Readonly
			}
		}
	}
	e.reg.Mutate(cp, func(s *station.Station) {
		for k, v := range cfg {
			s.Config[k] = v
		}
		caps := &s.Capabilities
		if n, err := strconv.Atoi(cfg["ChargeProfileMaxStackLevel"]); err == nil {
			caps.MaxStackLevel = n
		}
		caps.RateUnit = rateUnit(cfg["ChargingScheduleAllowedChargingRateUnit"])
		caps.PhaseSwitching = isTrue(cfg["ConnectorSwitch3to1PhaseSupported"]) || isTrue(cfg["ACPhaseSwitchingSupported"])
		if fp, ok := cfg["SupportedFeatureProfiles"]; ok && fp != "" {
			caps.RemoteTrigger = strings.Contains(fp, "RemoteTrigger")
		}
	})

	// Measurands: one candidate at a time, then the accepted list.
	if readonly["MeterValuesSampledData"] {
		e.reg.AddStep(cp, "ChangeConfiguration", "MeterValuesSampledData is read-only", "skipped")
	} else {
		maxLen, _ := strconv.Atoi(cfg["MeterValuesSampledDataMaxLength"])
		okList := detect(candidates(o.Measurands), maxLen, func(m string) bool {
			_, st, ok := e.step(ctx, cp, "ChangeConfiguration", "MeterValuesSampledData="+m, map[string]any{"key": "MeterValuesSampledData", "value": m})
			return ok && accepted(st)
		})
		if len(okList) > 0 {
			list := strings.Join(okList, ",")
			if _, st, ok := e.step(ctx, cp, "ChangeConfiguration", "MeterValuesSampledData="+list, map[string]any{"key": "MeterValuesSampledData", "value": list}); ok && accepted(st) {
				e.reg.Mutate(cp, func(s *station.Station) {
					s.Capabilities.Measurands = okList
					s.Config["MeterValuesSampledData"] = list
				})
			}
		}
	}
	if o.MeterInterval > 0 {
		secs := strconv.Itoa(int(o.MeterInterval.Seconds()))
		e.step(ctx, cp, "ChangeConfiguration", "MeterValueSampleInterval="+secs, map[string]any{"key": "MeterValueSampleInterval", "value": secs})
	}
	if o.PingInterval > 0 {
		secs := strconv.Itoa(int(o.PingInterval.Seconds()))
		e.step(ctx, cp, "ChangeConfiguration", "WebSocketPingInterval="+secs, map[string]any{"key": "WebSocketPingInterval", "value": secs})
	}

	// Ask for the current state.
	if st, _ := e.reg.Snapshot(cp); st.Capabilities.RemoteTrigger {
		n := 1
		if v, err := strconv.Atoi(cfg["NumberOfConnectors"]); err == nil && v > 0 {
			n = v
		}
		e.step(ctx, cp, "TriggerMessage", "StatusNotification connector 0", map[string]any{"requestedMessage": "StatusNotification", "connectorId": 0})
		for c := 1; c <= n; c++ {
			e.step(ctx, cp, "TriggerMessage", fmt.Sprintf("StatusNotification connector %d", c), map[string]any{"requestedMessage": "StatusNotification", "connectorId": c})
			e.step(ctx, cp, "TriggerMessage", fmt.Sprintf("MeterValues connector %d", c), map[string]any{"requestedMessage": "MeterValues", "connectorId": c})
		}
	}
}

func (e *Ext) setup2(ctx context.Context, cp string) {
	o := &e.Opts
	if !o.SkipAvailability {
		e.step(ctx, cp, "ChangeAvailability", "charging station Operative", map[string]any{"operationalStatus": "Operative"})
	}

	var get []map[string]any
	for _, v := range variables2 {
		get = append(get, map[string]any{"component": map[string]any{"name": v[0]}, "variable": map[string]any{"name": v[1]}})
	}
	resp, _, ok := e.step(ctx, cp, "GetVariables", "controller variables", map[string]any{"getVariableData": get})
	cfg := map[string]string{}
	if ok {
		var r struct {
			Result []struct {
				Status    string `json:"attributeStatus"`
				Value     string `json:"attributeValue"`
				Component struct {
					Name string `json:"name"`
				} `json:"component"`
				Variable struct {
					Name string `json:"name"`
				} `json:"variable"`
			} `json:"getVariableResult"`
		}
		if json.Unmarshal(resp, &r) == nil {
			for _, x := range r.Result {
				if x.Status == "Accepted" {
					cfg[x.Component.Name+"."+x.Variable.Name] = x.Value
				}
			}
		}
	}
	e.reg.Mutate(cp, func(s *station.Station) {
		for k, v := range cfg {
			s.Config[k] = v
		}
		caps := &s.Capabilities
		if n, err := strconv.Atoi(cfg["SmartChargingCtrlr.ProfileStackLevel"]); err == nil {
			caps.MaxStackLevel = n
		}
		caps.RateUnit = rateUnit(cfg["SmartChargingCtrlr.RateUnit"])
		caps.PhaseSwitching = isTrue(cfg["SmartChargingCtrlr.ACPhaseSwitchingSupported"])
	})

	set := func(comp, name, value string) bool {
		resp, _, ok := e.step(ctx, cp, "SetVariables", comp+"."+name+"="+value, map[string]any{"setVariableData": []map[string]any{{
			"attributeValue": value,
			"component":      map[string]any{"name": comp},
			"variable":       map[string]any{"name": name},
		}}})
		if !ok {
			return false
		}
		var r struct {
			Result []struct {
				Status string `json:"attributeStatus"`
			} `json:"setVariableResult"`
		}
		return json.Unmarshal(resp, &r) == nil && len(r.Result) > 0 && accepted(r.Result[0].Status)
	}

	acc := detect(candidates(o.Measurands), 0, func(m string) bool {
		return set("SampledDataCtrlr", "TxUpdatedMeasurands", m)
	})
	if len(acc) > 0 {
		list := strings.Join(acc, ",")
		if set("SampledDataCtrlr", "TxUpdatedMeasurands", list) {
			e.reg.Mutate(cp, func(s *station.Station) { s.Capabilities.Measurands = acc })
		}
	}
	if o.MeterInterval > 0 {
		set("SampledDataCtrlr", "TxUpdatedInterval", strconv.Itoa(int(o.MeterInterval.Seconds())))
	}
	if o.PingInterval > 0 {
		set("OCPPCommCtrlr", "WebSocketPingInterval", strconv.Itoa(int(o.PingInterval.Seconds())))
	}
	if st, _ := e.reg.Snapshot(cp); st.Capabilities.RemoteTrigger {
		e.step(ctx, cp, "TriggerMessage", "StatusNotification", map[string]any{"requestedMessage": "StatusNotification"})
		e.step(ctx, cp, "TriggerMessage", "MeterValues", map[string]any{"requestedMessage": "MeterValues"})
	}
}

// detect tries the candidates one at a time and returns those try accepted,
// at most limit of them (limit <= 0 = unlimited).
func detect(cands []string, limit int, try func(string) bool) []string {
	var out []string
	for _, c := range cands {
		if limit > 0 && len(out) >= limit {
			break
		}
		if try(c) {
			out = append(out, c)
		}
	}
	return out
}

func accepted(status string) bool { return status == "Accepted" || status == "RebootRequired" }

func isTrue(s string) bool { return strings.EqualFold(strings.TrimSpace(s), "true") }

// rateUnit picks the charging rate unit from an allowed-units value such as
// "Current", "Power" or "A,W": amps unless only watts are allowed.
func rateUnit(allowed string) string {
	a := strings.ToLower(allowed)
	hasW := strings.Contains(a, "power") || containsToken(a, "w")
	hasA := strings.Contains(a, "current") || containsToken(a, "a")
	if hasW && !hasA {
		return "W"
	}
	return "A"
}

func containsToken(list, tok string) bool {
	for _, t := range strings.Split(list, ",") {
		if strings.TrimSpace(t) == tok {
			return true
		}
	}
	return false
}

func sortStations(list []station.Station) {
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
}
