package controller_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/controller"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/station"
	"github.com/shiv3/gocpp/core/ocppj"
)

func ptr[T any](v T) *T { return &v }

func TestBuildLimit(t *testing.T) {
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	opts := controller.Options{NominalVoltage: 230, ProfileID: 9}
	amps := station.Capabilities{RateUnit: "A", MaxStackLevel: -1}
	watts := station.Capabilities{RateUnit: "W", MaxStackLevel: 4}

	cases := []struct {
		name string
		v    ocppj.Version
		req  controller.LimitRequest
		caps station.Capabilities
		txn  string
		want string
	}{
		{"1.6 amps", ocppj.V16, controller.LimitRequest{ConnectorID: 1, Amps: ptr(16.0)}, amps, "",
			`{"connectorId":1,"csChargingProfiles":{"chargingProfileId":9,"chargingProfileKind":"Absolute","chargingProfilePurpose":"TxDefaultProfile","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"limit":16,"startPeriod":0}],"startSchedule":"2026-01-01T11:59:00Z"},"stackLevel":0}}`},
		{"1.6 watts unit from amps", ocppj.V16, controller.LimitRequest{ConnectorID: 1, Amps: ptr(10.0), Phases: ptr(1), Relative: true, StackLevel: ptr(2)}, watts, "",
			`{"connectorId":1,"csChargingProfiles":{"chargingProfileId":9,"chargingProfileKind":"Relative","chargingProfilePurpose":"TxDefaultProfile","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"limit":2300,"numberPhases":1,"startPeriod":0}]},"stackLevel":2}}`},
		{"1.6 tx pause", ocppj.V16, controller.LimitRequest{ConnectorID: 2, Watts: ptr(0.0), Purpose: "tx"}, watts, "77",
			`{"connectorId":2,"csChargingProfiles":{"chargingProfileId":9,"chargingProfileKind":"Absolute","chargingProfilePurpose":"TxProfile","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"limit":0,"startPeriod":0}],"startSchedule":"2026-01-01T11:59:00Z"},"stackLevel":4,"transactionId":77}}`},
		{"2.0.1 default", ocppj.V201, controller.LimitRequest{ConnectorID: 1, Watts: ptr(4140.0), Phases: ptr(3)}, amps, "",
			`{"chargingProfile":{"chargingProfileKind":"Absolute","chargingProfilePurpose":"TxDefaultProfile","chargingSchedule":[{"chargingRateUnit":"A","chargingSchedulePeriod":[{"limit":6,"numberPhases":3,"startPeriod":0}],"id":9,"startSchedule":"2026-01-01T11:59:00Z"}],"id":9,"stackLevel":0},"evseId":1}`},
		{"2.0.1 station max", ocppj.V201, controller.LimitRequest{ConnectorID: 1, Amps: ptr(32.0), Purpose: "max"}, amps, "",
			`{"chargingProfile":{"chargingProfileKind":"Absolute","chargingProfilePurpose":"ChargingStationMaxProfile","chargingSchedule":[{"chargingRateUnit":"A","chargingSchedulePeriod":[{"limit":32,"startPeriod":0}],"id":9,"startSchedule":"2026-01-01T11:59:00Z"}],"id":9,"stackLevel":0},"evseId":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := controller.BuildLimit(tc.v, tc.req, tc.caps, tc.txn, opts, now)
			if err != nil {
				t.Fatal(err)
			}
			got, _ := json.Marshal(plan.Payload)
			if string(got) != tc.want {
				t.Errorf("payload\n got %s\nwant %s", got, tc.want)
			}
		})
	}

	bad := []controller.LimitRequest{
		{ConnectorID: 1},
		{ConnectorID: 1, Amps: ptr(1.0), Watts: ptr(1.0)},
		{ConnectorID: 1, Amps: ptr(-1.0)},
		{ConnectorID: 1, Amps: ptr(6.0), Phases: ptr(2)},
		{ConnectorID: 1, Amps: ptr(6.0), Purpose: "other"},
	}
	for i, req := range bad {
		if _, err := controller.BuildLimit(ocppj.V16, req, amps, "", opts, now); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
	if _, err := controller.BuildLimit(ocppj.V16, controller.LimitRequest{ConnectorID: 1, Amps: ptr(6.0), Purpose: "tx"}, amps, "", opts, now); err == nil {
		t.Error("tx without a transaction accepted")
	}
}
