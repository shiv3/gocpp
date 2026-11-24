#!/usr/bin/env bash
# Fails on ANY difference between the exported API of every package of the
# pinned upstream tag and of this module: additions, removals and changes alike.
# Intentional, reviewed deviations (schema conformance fixes where upstream's
# types contradict the OCA JSON schemas) are listed one apidiff line each in
# scripts/apidiff-allow.txt, with a comment citing the schema or upstream issue.
# An entry that no longer matches any difference fails too, so the list cannot rot.
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
allow="$here/scripts/apidiff-allow.txt"
used="$tmp/used"
: > "$used"
status=0
n=0
for p in $pkgs; do
  base="$tmp/$(echo "$p" | tr '/' '_').export"
  (cd "$src" && apidiff -w "$base" "$p")
  out="$(cd "$here" && apidiff "$base" "$p" 2>&1 || true)"
  n=$((n + 1))
  if [ -n "$out" ] && [ -f "$allow" ]; then
    # entries are "<import path>|<apidiff line>"; drop the ones for this package
    out="$(printf '%s\n' "$out" | ALLOW="$allow" PKG="$p" USED="$used" awk '
      BEGIN {
        while ((getline l < ENVIRON["ALLOW"]) > 0) {
          if (l ~ /^[[:space:]]*(#|$)/) continue
          i = index(l, "|"); if (i == 0) continue
          if (substr(l, 1, i - 1) == ENVIRON["PKG"]) ok[substr(l, i + 1)] = 1
        }
      }
      /^- / && ($0 in ok) { print ENVIRON["PKG"] "|" $0 >> ENVIRON["USED"]; next }
      { print }
    ')"
    # only section headers left: no difference
    printf '%s\n' "$out" | grep -q '^- ' || out=""
  fi
  if [ -n "$out" ]; then
    echo "== API difference in $p =="
    echo "$out"
    status=1
  fi
done
if [ -f "$allow" ]; then
  while IFS= read -r l; do
    case "$l" in ''|'#'*|' '*'#'*) continue ;; esac
    if ! grep -qxF -- "$l" "$used"; then
      echo "apidiff-allow.txt entry matches no difference (remove it): $l"
      status=1
    fi
  done < "$allow"
fi
[ "$status" = 0 ] && echo "apidiff: no difference in $n packages vs $UPSTREAM_TAG"
exit "$status"
