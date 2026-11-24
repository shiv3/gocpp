// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package ocpp16_test

import (
	"strings"
	"testing"

	"github.com/lorenzodonini/ocpp-go/internal/schematest"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/certificates"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/logging"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/smartcharging"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/types"
)

// See ocpp2.0.1_test/schema_conformance_test.go: round trips of the types that
// deviated from the OCA JSON schemas in upstream v0.19.0.

const v16 = "v16"

func validate(v any) error { return types.Validate.Struct(v) }

func roundTrip(t *testing.T, schema, sample string, newPtr func() any) {
	t.Helper()
	t.Run(schema, func(t *testing.T) { schematest.RoundTrip(t, v16, schema, sample, newPtr, validate) })
}

func TestSchemaConformanceChargingProfileTransactionId(t *testing.T) {
	profile := func(tx string) string {
		return `{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1,` + tx + `"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"A","chargingSchedulePeriod":[{"startPeriod":0,"limit":10}]}}}`
	}
	// absent, a positive id and 0 are three different messages
	roundTrip(t, "SetChargingProfile", profile(``), func() any { return &smartcharging.SetChargingProfileRequest{} })
	roundTrip(t, "SetChargingProfile", profile(`"transactionId":42,`), func() any { return &smartcharging.SetChargingProfileRequest{} })
	roundTrip(t, "SetChargingProfile", profile(`"transactionId":0,`), func() any { return &smartcharging.SetChargingProfileRequest{} })
}

// A 1.6-only program (this test binary links no OCPP 2.0.1 package) must validate
// certificate hashes instead of panicking with "Undefined validation function".
func TestSchemaConformanceCertificateHashData(t *testing.T) {
	roundTrip(t, "GetInstalledCertificateIdsResponse",
		`{"status":"Accepted","certificateHashData":[{"hashAlgorithm":"SHA256","issuerNameHash":"a","issuerKeyHash":"b","serialNumber":"1"}]}`,
		func() any { return &certificates.GetInstalledCertificateIdsResponse{} })
	bad := &certificates.GetInstalledCertificateIdsResponse{
		Status:              certificates.GetInstalledCertificateStatusAccepted,
		CertificateHashData: []types.CertificateHashData{{HashAlgorithm: "MD5", IssuerNameHash: "a", IssuerKeyHash: "b", SerialNumber: "1"}},
	}
	if err := validate(bad); err == nil {
		t.Error("an unknown hash algorithm must be refused")
	}
}

func TestSchemaConformanceGetLogResponse(t *testing.T) {
	roundTrip(t, "GetLogResponse", `{"status":"Accepted","filename":"`+strings.Repeat("a", 255)+`"}`,
		func() any { return &logging.GetLogResponse{} })
	long := &logging.GetLogResponse{Status: logging.LogStatusAccepted, Filename: strings.Repeat("a", 256)}
	if err := validate(long); err == nil {
		t.Error("a 256 character filename must be refused")
	}
}
