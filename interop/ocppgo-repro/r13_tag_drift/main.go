// r13: hand-written struct tags disagree with the OCA JSON schemas.
//
// Each case is a message that the official schema accepts. It is built with
// the library's own constructor and checked with the library's validator
// (the same check that runs on every inbound and outbound message).
// The pattern behind most of them: the tag `required` means "not the zero
// value", the schema's `required` means "property present", so 0, 0.0 and ""
// cannot be sent or received where the schema allows them.
package main

import (
	"fmt"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/diagnostics"
	"github.com/lorenzodonini/ocpp-go/ocpp2.0.1/tariffcost"
	"github.com/lorenzodonini/ocpp-go/ocppj"

	"github.com/shiv3/gocpp/interop/ocppgo-repro/h"
)

func main() {
	fmt.Printf("library: %s\n", h.Variant)
	cases := []struct {
		what   string
		schema string
		v      interface{}
	}{
		{"1.6 ChangeConfiguration.req value \"\" (clear a setting; upstream issue #246, open)",
			`ChangeConfiguration.json: "value": {"type":"string","maxLength":500}, required`,
			core.NewChangeConfigurationRequest("AuthorizationKey", "")},
		{"2.0.1 CostUpdatedRequest totalCost 0 (free session)",
			`CostUpdatedRequest.json: "totalCost": {"type":"number"}, required`,
			tariffcost.NewCostUpdatedRequest(0, "tx-1")},
		{"2.0.1 ClearVariableMonitoringResponse result id 0",
			`ClearVariableMonitoringResponse.json: "id": {"type":"integer"}, required`,
			diagnostics.ClearMonitoringResult{ID: 0, Status: diagnostics.ClearMonitoringStatusAccepted}},
	}
	rejected := 0
	for _, c := range cases {
		err := ocppj.Validate.Struct(c.v)
		verdict := "accepted"
		if err != nil {
			verdict = "REJECTED: " + err.Error()
			rejected++
		}
		fmt.Printf("- %s\n    schema: %s\n    ocpp-go: %s\n", c.what, c.schema, verdict)
	}
	h.Result(rejected > 0, "%d of %d schema-valid messages rejected by the struct tags", rejected, len(cases))
}
