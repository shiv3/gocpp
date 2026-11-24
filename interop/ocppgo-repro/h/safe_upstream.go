//go:build !compat

package h

// ApplySafe is a no-op on upstream: v0.19.0 has no option for a last-wins
// duplicate policy, an inbound size limit or a close frame on idle close.
func ApplySafe(cs *CS) bool { return false }
