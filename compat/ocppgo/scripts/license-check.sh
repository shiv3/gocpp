#!/usr/bin/env bash
# Licence and notice check of the compat module.
#
#   scripts/license-check.sh [UPSTREAM_CHECKOUT]
#
# 1. LICENSE is byte-identical to the licence file of the pinned upstream tag.
# 2. LICENSE.gocpp exists and is MIT.
# 3. NOTICE names the module path, the upstream licence and the UPSTREAM pin file.
# 4. Every Go file that does not exist in the pinned upstream tag carries the
#    gocpp header ("Licensed under the MIT License (LICENSE.gocpp)"), and every
#    file that does exist there carries no such header (it is upstream code, or a
#    patched copy of it, and stays under the upstream licence).
# 5. No dependency in go.mod outside the MIT/BSD/Apache-2.0 family is added
#    unnoticed: the list of module paths is compared with an allow list in
#    scripts/licenses.allow (one module path per line; an update is a reviewed change).
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
status=0
fail() { echo "license-check: $*" >&2; status=1; }

cmp -s "$src/LICENSE" "$here/LICENSE" || fail "LICENSE differs from $UPSTREAM_TAG's LICENSE"
grep -q "MIT License" "$here/LICENSE.gocpp" || fail "LICENSE.gocpp missing or not MIT"
for needle in "$UPSTREAM_MODULE" "UPSTREAM" "MIT License" "LICENSE.gocpp"; do
  grep -qF -- "$needle" "$here/NOTICE" || fail "NOTICE does not mention '$needle'"
done

cd "$here"
while IFS= read -r f; do
  f="${f#./}"
  case "$f" in vendor/*) continue ;; esac
  if [ -e "$src/$f" ]; then
    if head -n 5 "$f" | grep -q "LICENSE.gocpp"; then fail "$f is an upstream file but carries the gocpp header"; fi
  else
    head -n 5 "$f" | grep -q "Licensed under the MIT License (LICENSE.gocpp)" || fail "$f is not in upstream and lacks the gocpp header"
  fi
done < <(find . -name '*.go' -type f | sort)

if [ -f scripts/licenses.allow ]; then
  # direct and indirect requirements, minus the module itself
  deps="$(go list -m -f '{{if not .Main}}{{.Path}}{{end}}' all 2>/dev/null | sort -u)"
  while IFS= read -r d; do
    [ -z "$d" ] && continue
    grep -qxF -- "$d" scripts/licenses.allow || fail "dependency $d is not in scripts/licenses.allow (review its licence, then add it)"
  done <<<"$deps"
fi
[ "$status" = 0 ] && echo "license-check: ok"
exit "$status"
