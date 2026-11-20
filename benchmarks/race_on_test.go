//go:build race

package benchmarks

// raceEnabled reports whether the race detector is on; it inflates heap and
// stack numbers several-fold, so footprint bounds are not asserted under it.
const raceEnabled = true
