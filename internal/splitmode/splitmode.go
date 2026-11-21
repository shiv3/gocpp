// Package splitmode lets the conformance and e2e suites run a second time with
// the server split into a gateway and a processing tier joined by the
// in-process link: set GOCPP_TEST_SPLIT=1. Tests create their server with
// splitmode.NewServer instead of csms.NewServer; without the variable it is
// exactly csms.NewServer.
package splitmode

import (
	"os"

	"github.com/shiv3/gocpp/csms"
)

// Enabled reports whether the split run was requested.
func Enabled() bool { return os.Getenv("GOCPP_TEST_SPLIT") != "" }

// NewServer is csms.NewServer, plus csms.WithSplitInProcess in split mode.
func NewServer(opts ...csms.Option) *csms.Server {
	if Enabled() {
		opts = append(opts[:len(opts):len(opts)], csms.WithSplitInProcess())
	}
	return csms.NewServer(opts...)
}
