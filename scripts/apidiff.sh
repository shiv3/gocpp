#!/usr/bin/env bash
set -euo pipefail
# Compare the public API of the working tree against a git ref (default: latest tag),
# failing if any package has incompatible (breaking) changes.
BASE_REF="${1:-$(git describe --tags --abbrev=0)}"
go install golang.org/x/exp/cmd/apidiff@latest
APIDIFF="$(go env GOPATH)/bin/apidiff"

PKGS=(
  github.com/shiv3/gocpp/csms
  github.com/shiv3/gocpp/cp
  github.com/shiv3/gocpp/core/ocppj
  github.com/shiv3/gocpp/core/dispatcher
  github.com/shiv3/gocpp/core/schema
  github.com/shiv3/gocpp/core/storage
  github.com/shiv3/gocpp/core/auth
  github.com/shiv3/gocpp/core/observability
)

worktree="$(mktemp -d)"
git worktree add -q "$worktree" "$BASE_REF"
trap 'git worktree remove -f "$worktree" >/dev/null 2>&1 || true' EXIT

status=0
for p in "${PKGS[@]}"; do
  base="$(mktemp)"
  ( cd "$worktree" && "$APIDIFF" -w "$base" "$p" )
  out="$("$APIDIFF" "$base" "$p" || true)"
  if echo "$out" | grep -q "Incompatible changes"; then
    echo "== INCOMPATIBLE API change in $p (vs $BASE_REF) =="
    echo "$out"
    status=1
  fi
done

[ "$status" -eq 0 ] && echo "apidiff: no incompatible changes vs $BASE_REF"
exit "$status"
