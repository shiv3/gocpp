// Command gocpp-csms is a standalone test CSMS (central system) for OCPP 1.6,
// 2.0.1 and 2.1 charge points.
//
// It has two modes:
//
//	gocpp-csms --version 1.6|2.0.1|2.1   legacy e2e mode (frozen contract)
//	gocpp-csms serve [flags]             serve mode
//
// The legacy e2e mode is the CSMS of the ocpp-cp-simulator e2e suite: one OCPP
// version per process on ephemeral loopback ports, announced by the first
// stdout line, followed by one NDJSON line per received charge point CALL. Its
// interface is pinned by legacy_contract_test.go.
//
// Serve mode is a configurable CSMS for real and simulated charge points: one
// listener serves OCPP 1.6, 2.0.1 and 2.1, with optional TLS, mTLS and Basic
// Auth, health, readiness and Prometheus endpoints, a generic command API and
// schema-valid answers to every charge-point-initiated action.
//
// See docs/gocpp-csms.md.
package main

import (
	"os"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/legacy"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
)

// serveExtensions are the optional serve-mode features compiled into the
// binary. Each one is added from its own ext_<name>.go file in this directory
// (an init function appending to this slice), so features developed in
// parallel never edit the same file.
var serveExtensions []server.Extension

func main() {
	os.Exit(run())
}

func run() int {
	if len(os.Args) > 1 && os.Args[1] == "serve" {
		return server.Main(os.Args[2:], os.Getenv, os.Stdout, os.Stderr, serveExtensions...)
	}
	return legacy.Main(os.Args[1:])
}
