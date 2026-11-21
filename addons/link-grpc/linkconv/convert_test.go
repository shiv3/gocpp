package linkconv_test

import (
	"testing"
	"time"

	"github.com/shiv3/gocpp/addons/link-grpc/linkconv"
	"github.com/shiv3/gocpp/gateway"
)

// The relative budget of a call survives the wire (it replaces the absolute
// deadline across hosts whose clocks differ).
func TestCallRequestBudgetRoundTrip(t *testing.T) {
	in := gateway.CallRequest{CallID: "c", Action: "Reset", Deadline: time.Unix(100, 0), Budget: 7 * time.Second}
	out := linkconv.CallReqFromPB(linkconv.CallReqToPB(in))
	if out.Budget != in.Budget {
		t.Fatalf("budget = %v, want %v", out.Budget, in.Budget)
	}
}
