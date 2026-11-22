#!/usr/bin/env bash
# Fails on ANY difference between the exported API of every package of the
# pinned upstream tag and of this module: additions, removals and changes alike.
# (Compat-only switches live in environment variables and in internal packages,
# never in an existing package's API.)
#
#   scripts/apidiff.sh [UPSTREAM_CHECKOUT]
#
# apidiff does not compare struct tags (JSON and validation behaviour live in
# them). Every file that is not patched is byte-identical to upstream, which
# scripts/sync-upstream.sh --check verifies, and the patched files contain no
# tagged public types, so tags are covered as well.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../UPSTREAM
. "$here/UPSTREAM"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
src="${1:-}"
if [ -z "$src" ]; then
  git clone -q --depth 1 --branch "$UPSTREAM_TAG" "$UPSTREAM_REPO" "$tmp/up"
  src="$tmp/up"
fi
got="$(git -C "$src" rev-parse "HEAD^{commit}")"
if [ "$got" != "$UPSTREAM_COMMIT" ]; then
  echo "upstream checkout is at $got, expected $UPSTREAM_COMMIT ($UPSTREAM_TAG)" >&2
  exit 2
fi
if ! command -v apidiff >/dev/null 2>&1; then
  go install golang.org/x/exp/cmd/apidiff@latest
  PATH="$PATH:$(go env GOPATH)/bin"
fi
# Packages that have non-test Go files.
pkgs="$(cd "$src" && go list -f '{{if .GoFiles}}{{.ImportPath}}{{end}}' ./... | grep -v '/internal/' )"
status=0
n=0
for p in $pkgs; do
  base="$tmp/$(echo "$p" | tr '/' '_').export"
  (cd "$src" && apidiff -w "$base" "$p")
  out="$(cd "$here" && apidiff "$base" "$p" 2>&1 || true)"
  n=$((n + 1))
  if [ -n "$out" ]; then
    echo "== API difference in $p =="
    echo "$out"
    status=1
  fi
done
[ "$status" = 0 ] && echo "apidiff: no difference in $n packages vs $UPSTREAM_TAG"
exit "$status"
