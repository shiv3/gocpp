// Package conf201a contains OCPP 2.0.1 per-message conformance tests.
package conf201a

import (
	"time"

	"github.com/shiv3/gocpp/v201/messages"
)

func strPtr(s string) *string {
	return &s
}

func int32Ptr(i int32) *int32 {
	return &i
}

func testTime() time.Time {
	return time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
}

func testCustomData() *messages.CustomDataType {
	return &messages.CustomDataType{VendorID: "vendor"}
}

func testStatusInfo() *messages.StatusInfoType {
	return &messages.StatusInfoType{
		AdditionalInfo: strPtr("accepted"),
		ReasonCode:     "OK",
	}
}
