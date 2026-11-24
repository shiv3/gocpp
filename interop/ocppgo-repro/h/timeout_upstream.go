//go:build !compat

package h

import (
	"time"

	"github.com/lorenzodonini/ocpp-go/ocpp1.6/core"
	"github.com/lorenzodonini/ocpp-go/ocpp1.6/remotetrigger"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// Variant names the library the program was built against.
const Variant = "ocpp-go v0.19.0 (upstream)"

// newEndpoint sets the call timeout the only way upstream offers: by
// injecting a DefaultServerDispatcher (ocppj.NewServer(ws, dispatcher, nil, ...)).
func newEndpoint(srv *ws.Server, callTimeout time.Duration) *ocppj.Server {
	d := ocppj.NewDefaultServerDispatcher(ocppj.NewFIFOQueueMap(0))
	if callTimeout > 0 {
		d.SetTimeout(callTimeout)
	}
	return ocppj.NewServer(srv, d, nil, core.Profile, remotetrigger.Profile)
}
