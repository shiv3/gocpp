//go:build compat

package h

import (
	"time"

	"github.com/lorenzodonini/ocpp-go/compat"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// Variant names the library the program was built against.
const Variant = "gocpp drop-in (compat/ocppgo, same source, one replace line)"

// newEndpoint: an injected dispatcher would select the module's legacy
// runtime, so the timeout is set through the compat package instead.
func newEndpoint(srv *ws.Server, callTimeout time.Duration) *ocppj.Server {
	e := ocppj.NewServer(srv, nil, nil, core.Profile, remotetrigger.Profile)
	if callTimeout > 0 {
		compat.SetCallTimeout(e, callTimeout)
	}
	return e
}
