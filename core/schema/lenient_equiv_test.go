package schema_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/v16"
)

// payloadCases are the payloads the old/new lenient comparison runs on: valid
// messages, the malformed and quirky payloads of the interop parity cases, and
// JSON corner cases (escapes, duplicates, odd numbers, trailing data, nesting).
var payloadCases = []struct{ action, kind, payload string }{
	{"Heartbeat", "request", `{}`},
	{"Heartbeat", "request", ` { } `},
	{"Heartbeat", "request", "\t{\n}\r\n"},
	{"Heartbeat", "request", `{"x":1}`},
	{"Heartbeat", "request", `null`},
	{"Heartbeat", "request", `[]`},
	{"Heartbeat", "request", `{} trailing`},
	{"Heartbeat", "request", `{}{}`},
	{"Heartbeat", "request", ``},
	{"Heartbeat", "request", `{`},
	{"Heartbeat", "request", `not json`},
	{"Heartbeat", "response", `{"currentTime":"2026-01-01T00:00:00Z"}`},
	{"Heartbeat", "response", `{"currentTime":"2026-01-01 00:00:00"}`},
	{"Heartbeat", "response", `{"currentTime":"2026-01-01T09:00:00.123+09:00"}`},
	{"Heartbeat", "response", `{"currentTime":"2026-01-01T00:00:00"}`},
	{"Heartbeat", "response", `{"currentTime":"2026-01-01T00:00:00.123456789Z"}`},
	{"Heartbeat", "response", `{}`},
	{"Heartbeat", "response", `{"currentTime":12}`},
	{"BootNotification", "request", `{"chargePointVendor":"V","chargePointModel":"M"}`},
	{"BootNotification", "request", `{"chargePointVendor":"V","chargePointModel":"M","bogus":1}`},
	{"BootNotification", "request", `{"chargePointModel":"M"}`},
	{"BootNotification", "request", `{"chargePointVendor":"Vé","chargePointModel":"M\n"}`},
	{"BootNotification", "request", "{\"chargePointVendor\":\"V\xff\",\"chargePointModel\":\"M\"}"},
	{"BootNotification", "request", `{"chargePointVendor":"日本語","chargePointModel":"M"}`},
	{"BootNotification", "request", `{"chargePointVendor":"V","chargePointModel":"M","chargePointVendor":"W"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":"1","errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":1.0,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":1e0,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":-0,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":99999999999999999999,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":" 1","errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":"01","errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"noerror","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Foo"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"available","timestamp":"2026-01-01 00:00:00"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Available","timestamp":"2026-01-01T00:00:00Z"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Available","info":null}`},
	{"StatusNotification", "request", `{"connectorId":true,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Available"} {"x":1}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Available",}`},
	{"StatusNotification", "request", `{"connectorId":01,"errorCode":"NoError","status":"Available"}`},
	{"StatusNotification", "request", `{"connectorId":1,"errorCode":"NoError","status":"Available"}`},
	{"Authorize", "request", `{"idTag":"` + strings.Repeat("a", 21) + `"}`},
	{"Authorize", "request", `{"idTag":"` + strings.Repeat("a", 20) + `"}`},
	{"Authorize", "request", `{}`},
	{"Authorize", "request", `{"idTag":5}`},
	{"StartTransaction", "request", `{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01 00:00:00"}`},
	{"StartTransaction", "request", `{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T09:00:00.123+09:00"}`},
	{"StartTransaction", "request", `{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00"}`},
	{"StartTransaction", "request", `{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00.123456789Z"}`},
	{"StartTransaction", "request", `{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}`},
	{"StartTransaction", "request", `{"connectorId":"1","idTag":"T","meterStart":"0","timestamp":"2026-01-01T00:00:00Z","reservationId":"7"}`},
	{"StopTransaction", "request", `{"meterStop":1,"timestamp":"2026-01-01T00:00:00Z","transactionId":1}`},
	{"StopTransaction", "request", `{"meterStop":1,"timestamp":"2026-01-01T00:00:00Z","transactionId":1,"reason":"local","transactionData":[]}`},
	{"MeterValues", "request", `{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":123}]}]}`},
	{"MeterValues", "request", `{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T00:00:00Z","sampledValue":[{"value":"123","unit":"wh"}]}]}`},
	{"MeterValues", "request", `{"connectorId":1,"transactionId":4242,"meterValue":[{"timestamp":"2026-01-01 00:00:00","sampledValue":[{"value":"1","measurand":"energy.active.import.register"},{"value":"2","measurand":"Power.Active.Import","unit":"kW"}]},{"timestamp":"2026-01-01T00:00:01Z","sampledValue":[]}]}`},
	{"MeterValues", "request", `{"connectorId":1,"meterValue":[]}`},
	{"MeterValues", "request", `{"connectorId":1,"meterValue":{}}`},
	{"MeterValues", "request", `{"connectorId":1,"meterValue":[[]]}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":{"a":1}}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":[1,2, {"b" : "c"}]}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":true}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":"plain"}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":42}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":null}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":{"a":"é","n":1.50}}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":` + strings.Repeat("[", 80) + strings.Repeat("]", 80) + `}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":` + strings.Repeat("[", 70000) + strings.Repeat("]", 70000) + `}`},
	{"DataTransfer", "request", `{"vendorId":"v","data":"` + strings.Repeat("a", 5000) + `"}`},
	{"DataTransfer", "response", `{"status":"Accepted","data":{"k":[1,2]}}`},
	{"DataTransfer", "response", `{"status":"accepted"}`},
	{"GetConfiguration", "response", `{"configurationKey":[{"key":"A","readonly":"true","value":"1"},{"key":"B","readonly":false}],"unknownKey":["x"]}`},
	{"GetConfiguration", "response", `{"configurationKey":[{"key":"A","readonly":"TRUE"}]}`},
	{"GetConfiguration", "response", `{"configurationKey":[{"key":"A","readonly":"yes"}]}`},
	{"ChangeConfiguration", "request", `{"key":"k","value":"v"}`},
	{"ChangeConfiguration", "request", `{"key":"k","value":5}`},
	{"RemoteStartTransaction", "request", `{"idTag":"T","connectorId":"2"}`},
	{"SetChargingProfile", "request", `{"connectorId":0,"csChargingProfiles":{"chargingProfileId":1,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":16.5,"numberPhases":3}]}}}`},
	{"SetChargingProfile", "request", `{"connectorId":0,"csChargingProfiles":{"chargingProfileId":"1","stackLevel":0,"chargingProfilePurpose":"txdefaultprofile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":"16.5"}]}}}`},
	{"SetChargingProfile", "request", `{"connectorId":0,"csChargingProfiles":{"chargingProfileId":1,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":1e400}]}}}`},
	{"Reset", "request", `{"type":"soft"}`},
	{"Reset", "request", `{"type":"Soft"}`},
	{"Reset", "request", `{"type":"Hard","extra":{"deep":[1,{"x":null}]}}`},
	{"NoSuchAction", "request", `{}`},
}

func payloadValidators(t testing.TB) map[[2]string]*schema.Validator {
	t.Helper()
	reg := schema.NewRegistry()
	if err := v16.RegisterSchemas(reg); err != nil {
		t.Fatal(err)
	}
	out := map[[2]string]*schema.Validator{}
	for _, a := range []string{"Heartbeat", "BootNotification", "StatusNotification", "Authorize", "StartTransaction", "StopTransaction",
		"MeterValues", "DataTransfer", "GetConfiguration", "ChangeConfiguration", "RemoteStartTransaction", "SetChargingProfile", "Reset"} {
		for _, k := range []string{"request", "response"} {
			if v, ok := reg.Lookup("1.6", a, k); ok {
				out[[2]string{a, k}] = v
			}
		}
	}
	return out
}

var addlProps = regexp.MustCompile(`additional properties (.*) not allowed`)

// normalizeErr makes a validator message comparable: jsonschema reports sibling
// violations and the names inside "additional properties" in map iteration order,
// which differs from run to run (the same input already yields different texts in
// one implementation), so both are sorted.
func normalizeErr(s string) string {
	s = addlProps.ReplaceAllStringFunc(s, func(m string) string {
		names := strings.Split(addlProps.FindStringSubmatch(m)[1], ", ")
		sort.Strings(names)
		return "additional properties " + strings.Join(names, ", ") + " not allowed"
	})
	lines := strings.Split(s, "\n")
	if len(lines) > 1 {
		sort.Strings(lines[1:])
	}
	return strings.Join(lines, "\n")
}

func errString(err error) string {
	if err == nil {
		return "<nil>"
	}
	s := normalizeErr(err.Error())
	var ve *schema.ViolationError
	if errors.As(err, &ve) && strings.Count(s, "\n- at ") <= 1 {
		// With several violations the "first" leaf (hence Keyword) depends on map order.
		s += "|kw=" + ve.Keyword
		if ve.AtRoot {
			s += "|root"
		}
	}
	return s
}

// sameLenient fails when the production lenient path and the reference differ.
func sameLenient(t testing.TB, v *schema.Validator, raw []byte, opts schema.LenientOptions) {
	t.Helper()
	gotOut, gotSoft, gotErr := v.ValidateLenientWith(append([]byte(nil), raw...), opts)
	wantOut, wantSoft, wantErr := schema.RefValidateLenient(v, append([]byte(nil), raw...), opts)
	if errString(gotErr) != errString(wantErr) {
		t.Fatalf("payload %q: error differs\n got:  %s\n want: %s", raw, errString(gotErr), errString(wantErr))
	}
	if !bytes.Equal(gotOut, wantOut) || (gotOut == nil) != (wantOut == nil) {
		t.Fatalf("payload %q: output differs\n got:  %q\n want: %q", raw, gotOut, wantOut)
	}
	if !reflect.DeepEqual(gotSoft, wantSoft) {
		t.Fatalf("payload %q: soft differs\n got:  %v\n want: %v", raw, gotSoft, wantSoft)
	}
}

func TestLenientDifferential_Corpus(t *testing.T) {
	vs := payloadValidators(t)
	for _, c := range payloadCases {
		if v, ok := vs[[2]string{c.action, c.kind}]; ok {
			sameLenient(t, v, []byte(c.payload), schema.LenientOptions{})
		}
	}
	// Every payload against every validator: mostly rejections, which must be
	// rejected for the same reason.
	for _, c := range payloadCases {
		for _, v := range vs {
			sameLenient(t, v, []byte(c.payload), schema.LenientOptions{})
		}
	}
}

func TestLenientDifferential_TimeLocation(t *testing.T) {
	vs := payloadValidators(t)
	loc := schemaTestLocation(t)
	for _, c := range payloadCases {
		if v, ok := vs[[2]string{c.action, c.kind}]; ok {
			sameLenient(t, v, []byte(c.payload), schema.LenientOptions{TimeLocation: loc})
		}
	}
}

// FuzzLenientEquivalence: production lenient path vs the pre-rewrite reference,
// on arbitrary bytes against a fixed set of real schemas.
func FuzzLenientEquivalence(f *testing.F) {
	for i, c := range payloadCases {
		if len(c.payload) < 4096 {
			f.Add(uint8(i), []byte(c.payload))
		}
	}
	vs := payloadValidators(f)
	var list []*schema.Validator
	for _, c := range payloadCases {
		if v, ok := vs[[2]string{c.action, c.kind}]; ok {
			list = append(list, v)
		}
	}
	f.Fuzz(func(t *testing.T, sel uint8, raw []byte) {
		sameLenient(t, list[int(sel)%len(list)], raw, schema.LenientOptions{})
	})
}

// FuzzDecodeEquivalence: whenever the fast decoder takes an input, its tree is
// exactly what json.Decoder with UseNumber produces.
func FuzzDecodeEquivalence(f *testing.F) {
	for _, c := range payloadCases {
		if len(c.payload) < 4096 {
			f.Add([]byte(c.payload))
		}
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		got, ok := schema.FastDecode(raw)
		if !ok {
			return
		}
		dec := json.NewDecoder(bytes.NewReader(raw))
		dec.UseNumber()
		var want any
		if err := dec.Decode(&want); err != nil {
			t.Fatalf("fast decoder accepted %q but json.Decoder rejects it: %v", raw, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tree differs for %q\n got:  %#v\n want: %#v", raw, got, want)
		}
	})
}

func TestFastDecode_TakesCommonPayloads(t *testing.T) {
	for _, c := range payloadCases {
		if strings.Contains(c.payload, `\`) || strings.Contains(c.payload, "[[[[") || !strings.HasPrefix(c.payload, "{") || c.payload == "{" {
			continue
		}
		if _, ok := schema.FastDecode([]byte(c.payload)); !ok && json.Valid([]byte(c.payload)) &&
			!strings.Contains(c.payload, "\xff") && len(c.payload) < 1000 {
			t.Errorf("fast decoder fell back on a plain valid payload: %q", c.payload)
		}
	}
}

func schemaTestLocation(t testing.TB) *time.Location {
	t.Helper()
	return time.FixedZone("JST", 9*3600)
}
