// Package splitmode lets the conformance and e2e suites run a second time with
// the server split into a gateway and a processing tier. GOCPP_TEST_SPLIT=1
// joins them by the in-process link; GOCPP_TEST_SPLIT=<name> uses a transport
// registered under that name (Register), which is how the suites run over NATS
// without the main module depending on it (scripts/split-nats-suite.sh).
// Tests create their server with splitmode.NewServer instead of
// csms.NewServer; without the variable it is exactly csms.NewServer.
package splitmode

import (
	"fmt"
	"os"
	"sync"

	"github.com/shiv3/gocpp/csms"
)

var (
	mu    sync.Mutex
	links = map[string]func() csms.SplitLink{}
)

// Register makes a transport selectable as GOCPP_TEST_SPLIT=name. newLink is
// called once per server and must return a fresh link.
func Register(name string, newLink func() csms.SplitLink) {
	mu.Lock()
	defer mu.Unlock()
	links[name] = newLink
}

// Enabled reports whether the split run was requested.
func Enabled() bool { return os.Getenv("GOCPP_TEST_SPLIT") != "" }

// NewServer is csms.NewServer, plus a split topology in split mode.
func NewServer(opts ...csms.Option) *csms.Server {
	switch mode := os.Getenv("GOCPP_TEST_SPLIT"); mode {
	case "":
	case "1", "inproc":
		opts = append(opts[:len(opts):len(opts)], csms.WithSplitInProcess())
	default:
		mu.Lock()
		f := links[mode]
		mu.Unlock()
		if f == nil {
			panic(fmt.Sprintf("splitmode: GOCPP_TEST_SPLIT=%q names no registered transport", mode))
		}
		opts = append(opts[:len(opts):len(opts)], csms.WithSplitLink(f()))
	}
	return csms.NewServer(opts...)
}
