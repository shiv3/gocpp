//go:build race

package linkgrpc_test

// raceEnabled: the race detector inflates latency, so timing gates skip.
const raceEnabled = true
