// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package ocpp2_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/santhosh-tekuri/jsonschema/v6"

	"github.com/lorenzodonini/ocpp-go/internal/schematest"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/authorization"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/availability"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/data"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/diagnostics"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/display"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/firmware"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/iso15118"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/localauth"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/meter"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/provisioning"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/remotecontrol"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/reservation"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/security"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/tariffcost"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/transactions"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/types"
)

func schemaDefinesCustomData(t *testing.T, name string) bool {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	path := filepath.Join(filepath.Dir(file), "..", "..", "..", "schemas", "v201", name+".json")
	raw, err := jsonschema.NewCompiler().Compile(path)
	if err != nil {
		t.Fatalf("compile %s: %v", path, err)
	}
	_, ok := raw.Properties["customData"]
	return ok
}

// Every request and response of every profile has the customData property the
// schema defines on it, and arbitrary vendor properties inside it survive.
func TestCustomDataOnEveryMessage(t *testing.T) {
	profiles := []*ocpp.Profile{
		authorization.Profile, availability.Profile, data.Profile, diagnostics.Profile, display.Profile,
		firmware.Profile, iso15118.Profile, localauth.Profile, meter.Profile, provisioning.Profile,
		remotecontrol.Profile, reservation.Profile, security.Profile, smartcharging.Profile,
		tariffcost.Profile, transactions.Profile,
	}
	const in = `{"customData":{"vendorId":"acme","flag":true,"nested":{"a":[1,2]},"n":7}}`
	n := 0
	for _, p := range profiles {
		for _, f := range p.Features {
			for _, c := range []struct {
				suffix string
				typ    reflect.Type
			}{{"Request", f.GetRequestType()}, {"Response", f.GetResponseType()}} {
				name := f.GetFeatureName() + c.suffix
				n++
				if !schemaDefinesCustomData(t, name) {
					t.Errorf("%s: the schema has no top-level customData; %s must not have one either", name, c.typ)
					continue
				}
				ptr := reflect.New(c.typ)
				if err := json.Unmarshal([]byte(in), ptr.Interface()); err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				cd := ptr.Elem().FieldByName("CustomData")
				if !cd.IsValid() {
					t.Errorf("%s: %s has no CustomData field", name, c.typ)
					continue
				}
				got, ok := cd.Interface().(*types.CustomData)
				if !ok || got == nil || got.VendorID != "acme" || len(got.Extra) != 3 {
					t.Errorf("%s: customData not decoded: %#v", name, cd.Interface())
					continue
				}
				out, err := json.Marshal(ptr.Interface())
				if err != nil {
					t.Errorf("%s: %v", name, err)
					continue
				}
				var a, b any
				_ = json.Unmarshal([]byte(in), &a)
				_ = json.Unmarshal(out, &b)
				// other fields are zero values of required properties: compare only customData
				if !reflect.DeepEqual(a.(map[string]any)["customData"], b.(map[string]any)["customData"]) {
					t.Errorf("%s: customData changed: %s", name, out)
				}
			}
		}
	}
	if n < 100 {
		t.Errorf("only %d messages checked, the profile list is incomplete", n)
	}
}

func TestSchemaConformanceCustomData(t *testing.T) {
	runConformance(t, []conformanceCase{
		{"AuthorizeRequest",
			`{"customData":{"vendorId":"acme","k":"v"},"idToken":{"customData":{"vendorId":"acme"},"idToken":"1234","type":"KeyCode"}}`,
			func() any { return &authorization.AuthorizeRequest{} }},
		{"HeartbeatRequest",
			`{"customData":{"vendorId":"acme","x":{"y":1}}}`,
			func() any { return &availability.HeartbeatRequest{} }},
		{"GetCompositeScheduleResponse",
			`{"status":"Accepted","customData":{"vendorId":"acme"},"schedule":{"customData":{"vendorId":"acme"},"chargingSchedulePeriod":[{"customData":{"vendorId":"acme"},"startPeriod":0,"limit":6}],"evseId":1,"duration":60,"scheduleStart":"2025-01-01T10:00:00Z","chargingRateUnit":"A"}}`,
			func() any { return &smartcharging.GetCompositeScheduleResponse{} }},
	})
}

func TestCustomDataValidation(t *testing.T) {
	ok := &authorization.AuthorizeRequest{IdToken: types.IdToken{IdToken: "1", Type: types.IdTokenTypeKeyCode}, CustomData: &types.CustomData{VendorID: "acme"}}
	if err := validate(ok); err != nil {
		t.Errorf("valid customData rejected: %v", err)
	}
	ok.CustomData = &types.CustomData{} // vendorId is required by the schema
	if err := validate(ok); err == nil {
		t.Error("customData without vendorId must be refused")
	}
	if err := json.Unmarshal([]byte(`{"customData":{"vendorId":"a"}}`), &availability.HeartbeatRequest{}); err != nil {
		t.Error(err)
	}
	schematest.Invalid(t, v201, "HeartbeatRequest", []byte(`{"customData":{}}`))
}
