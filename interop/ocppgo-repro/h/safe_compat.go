//go:build compat

package h

import (
	"os"

	"github.com/lorenzodonini/ocpp-go/compat"
)

// ApplySafe turns on the drop-in's opt-in safer behaviours when REPRO_SAFE=1
// (./run.sh rNN compat-safe). By default the drop-in keeps upstream's
// charger-visible behaviour (first-wins duplicates, no size limit).
func ApplySafe(cs *CS) bool {
	if os.Getenv("REPRO_SAFE") != "1" {
		return false
	}
	compat.SetDuplicatePolicy(cs.WS, compat.DuplicateLastWins)
	compat.SetMaxMessageSize(cs.WS, 1<<20)
	compat.SetIdleClose(cs.WS, compat.IdleCloseFrame)
	return true
}
