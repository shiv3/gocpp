#!/usr/bin/env bash
# Run the conformance and e2e suites in split mode over NATS: a gateway and a
# processing tier per test server, joined by a real nats-server on loopback.
#
# The main module must not depend on addons/link-nats, so the transport is
# plugged in from outside: a throwaway go.work joins the two modules and an
# overlay adds one file to internal/splitmode that registers the NATS link.
# Nothing in the repository changes.
#
#   scripts/split-nats-suite.sh [go test args...]     (default: -race ./internal/...)
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

cat > "$tmp/register.go" <<'GO'
package splitmode

import (
	"github.com/shiv3/gocpp/addons/link-nats/splitharness"
	"github.com/shiv3/gocpp/csms"
)

func init() {
	Register("nats", func() csms.SplitLink { return splitharness.New() })
}
GO
# The e2e TestMain verifies goroutines at exit (goleak). The suites do not close
# every server they create, which costs nothing in process but leaves the NATS
# connections of the link open, so the leak check is skipped here; the in-process
# split run (GOCPP_TEST_SPLIT=1) keeps it.
cat > "$tmp/main_test.go" <<'GO'
package e2e

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(m.Run()) }
GO
cat > "$tmp/overlay.json" <<JSON
{"Replace": {
  "$root/internal/splitmode/zz_nats_register.go": "$tmp/register.go",
  "$root/internal/e2e/main_test.go": "$tmp/main_test.go"
}}
JSON

cd "$root"
cat > "$tmp/go.work" <<WORK
go 1.26.0

use (
	$root
	$root/addons/link-nats
	$root/addons/link-grpc
)
WORK
cp "$root/go.sum" "$tmp/go.work.sum"
export GOWORK="$tmp/go.work" GOCPP_TEST_SPLIT=nats
[ $# -gt 0 ] || set -- -race ./internal/...
exec go test -count=1 -overlay="$tmp/overlay.json" "$@"
