// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package ocpp2_test

import (
	"strings"
	"testing"
	"time"

	"github.com/lorenzodonini/ocpp-go/internal/schematest"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/authorization"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/diagnostics"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/iso15118"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/provisioning"
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

func TestSchemaConformanceSecurityDiagnosticsProvisioning(t *testing.T) {
	long := func(n int) string { return strings.Repeat("a", n) }
	runConformance(t, []conformanceCase{
		{"AuthorizeResponse",
			`{"idTokenInfo":{"status":"Accepted","evseId":[1,2]}}`,
			func() any { return &authorization.AuthorizeResponse{} }},
		{"AuthorizeRequest",
			`{"idToken":{"idToken":"1234","type":"KeyCode"},"iso15118CertificateHashData":[{"hashAlgorithm":"SHA256","issuerNameHash":"a","issuerKeyHash":"b","serialNumber":"1","responderURL":"http://ocsp.example.org"}]}`,
			func() any { return &authorization.AuthorizeRequest{} }},
		{"GetCertificateStatusRequest",
			`{"ocspRequestData":{"hashAlgorithm":"SHA256","issuerNameHash":"a","issuerKeyHash":"b","serialNumber":"1","responderURL":"http://ocsp.example.org"}}`,
			func() any { return &iso15118.GetCertificateStatusRequest{} }},
		{"GetCertificateStatusResponse",
			`{"status":"Failed","statusInfo":{"reasonCode":"ocspFailed"}}`,
			func() any { return &iso15118.GetCertificateStatusResponse{} }},
		{"GetCertificateStatusResponse",
			`{"status":"Accepted","ocspResult":"AAAA"}`,
			func() any { return &iso15118.GetCertificateStatusResponse{} }},
		{"LogStatusNotificationRequest",
			`{"status":"AcceptedCanceled","requestId":3}`,
			func() any { return &diagnostics.LogStatusNotificationRequest{} }},
		{"NotifyCustomerInformationRequest",
			`{"data":"","seqNo":0,"generatedAt":"2025-01-01T10:00:00Z","requestId":1}`,
			func() any { return &diagnostics.NotifyCustomerInformationRequest{} }},
		{"GetLogResponse",
			`{"status":"AcceptedCanceled","filename":"` + long(255) + `"}`,
			func() any { return &diagnostics.GetLogResponse{} }},
		{"GetVariablesResponse",
			`{"getVariableResult":[{"attributeStatus":"Rejected","attributeStatusInfo":{"reasonCode":"notSupported","additionalInfo":"x"},"component":{"name":"c"},"variable":{"name":"v"}},{"attributeStatus":"Accepted","attributeValue":"` + long(2500) + `","component":{"name":"c"},"variable":{"name":"v"}}]}`,
			func() any { return &provisioning.GetVariablesResponse{} }},
		{"GetInstalledCertificateIdsRequest",
			`{}`,
			func() any { return &iso15118.GetInstalledCertificateIdsRequest{} }},
		{"GetInstalledCertificateIdsRequest",
			`{"certificateType":["V2GRootCertificate","CSMSRootCertificate"]}`,
			func() any { return &iso15118.GetInstalledCertificateIdsRequest{} }},
	})
}

// "asking for all certificates" must not put "certificateType":null (or []) on the wire.
func TestSchemaConformanceGetInstalledCertificateIdsAll(t *testing.T) {
	for _, req := range []*iso15118.GetInstalledCertificateIdsRequest{
		{},
		{CertificateTypes: []types.CertificateUse{}},
	} {
		if data := schematest.Marshal(t, v201, "GetInstalledCertificateIdsRequest", req); string(data) != "{}" {
			t.Errorf("got %s, want {}", data)
		}
	}
}

// Values outside the schema are still rejected by the struct validation.
func TestSchemaConformanceStillRejects(t *testing.T) {
	bad := []any{
		&iso15118.GetCertificateStatusResponse{Status: "Rejected"},
		&diagnostics.LogStatusNotificationRequest{Status: "Accepted"},
		&diagnostics.GetLogResponse{Status: diagnostics.LogStatusAccepted, Filename: strings.Repeat("a", 256)},
		&provisioning.GetVariableResult{AttributeStatus: provisioning.GetVariableStatusAccepted, AttributeValue: strings.Repeat("a", 2501), Component: types.Component{Name: "c"}, Variable: types.Variable{Name: "v"}},
		&types.OCSPRequestDataType{HashAlgorithm: types.SHA256, IssuerNameHash: "a", IssuerKeyHash: "b", SerialNumber: "1"}, // responderURL is required
	}
	for _, v := range bad {
		if err := validate(v); err == nil {
			t.Errorf("%T: expected a validation error", v)
		}
	}
}
