package routertemporal

import "time"

// timeDuration keeps workflow inputs stable while avoiding direct exposure of
// Temporal SDK option structs in workflow history.
type timeDuration time.Duration

func (d timeDuration) Duration() time.Duration {
	return time.Duration(d)
}

func newTimeDuration(d time.Duration) timeDuration {
	return timeDuration(d)
}
