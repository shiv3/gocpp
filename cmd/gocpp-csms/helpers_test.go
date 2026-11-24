package main

import (
	"testing"
	"time"
)

// waitFor polls cond until it holds or 3 s passed (used by
// legacy_contract_test.go and serve_test.go).
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
