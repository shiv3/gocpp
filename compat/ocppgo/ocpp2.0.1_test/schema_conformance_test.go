package ocpp2_test

import (
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/internal/schematest"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/types"
)

// These tests check the struct types that deviated from the OCA OCPP 2.0.1 JSON
// schemas in upstream v0.19.0 (see README, "Breaking changes vs upstream v0.19.0"):
// a schema-valid message must unmarshal, pass the struct validation, marshal back
// to the same schema-valid message, and what the types produce must conform.

const v201 = "v201"

func validate(v any) error { return types.Validate.Struct(v) }

type conformanceCase struct {
	schema string
	sample string
	newPtr func() any
}

func runConformance(t *testing.T, cases []conformanceCase) {
	t.Helper()
	for _, c := range cases {
		c := c
		t.Run(c.schema, func(t *testing.T) {
			schematest.RoundTrip(t, v201, c.schema, c.sample, c.newPtr, validate)
		})
	}
}

func TestSchemaConformanceSmartCharging(t *testing.T) {
	runConformance(t, []conformanceCase{
		{"GetCompositeScheduleResponse",
			`{"status":"Accepted","statusInfo":{"reasonCode":"ok"},"schedule":{"chargingSchedulePeriod":[{"startPeriod":0,"limit":16.5,"numberPhases":1,"phaseToUse":2},{"startPeriod":600,"limit":8}],"evseId":1,"duration":1200,"scheduleStart":"2025-01-01T10:00:00Z","chargingRateUnit":"A"}}`,
			func() any { return &smartcharging.GetCompositeScheduleResponse{} }},
		{"GetCompositeScheduleResponse",
			`{"status":"Rejected"}`,
			func() any { return &smartcharging.GetCompositeScheduleResponse{} }},
		{"SetChargingProfileRequest",
			`{"evseId":1,"chargingProfile":{"id":1,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":[{"id":1,"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":10,"numberPhases":1,"phaseToUse":3}]}]}}`,
			func() any { return &smartcharging.SetChargingProfileRequest{} }},
	})
}

// A GetCompositeScheduleResponse built with the Go API conforms to the schema.
func TestSchemaConformanceCompositeScheduleMarshal(t *testing.T) {
	period := types.NewChargingSchedulePeriod(0, 10)
	period.PhaseToUse = new(int)
	*period.PhaseToUse = 1
	resp := smartcharging.NewGetCompositeScheduleResponse(smartcharging.GetCompositeScheduleStatusAccepted)
	resp.Schedule = &smartcharging.CompositeSchedule{
		ChargingSchedulePeriod: []types.ChargingSchedulePeriod{period},
		EvseID:                 0,
		Duration:               60,
		ScheduleStart:          types.NewDateTime(time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)),
		ChargingRateUnit:       types.ChargingRateUnitAmperes,
	}
	if err := validate(resp); err != nil {
		t.Fatal(err)
	}
	schematest.Marshal(t, v201, "GetCompositeScheduleResponse", resp)
}
