#!/usr/bin/env bash
# Re-creates the vendored upstream files of this module from the pinned tag.
#
#   scripts/sync-upstream.sh [--check] [UPSTREAM_CHECKOUT]
#
# 1. takes a pristine copy of the pinned tag (UPSTREAM_CHECKOUT, or a fresh
#    clone of UPSTREAM_REPO at UPSTREAM_TAG),
# 2. applies patches/*.patch to it (every change this module makes to an
#    upstream file; files that exist only here are not patches),
# 3. copies the result over the module directory, or with --check reports any
#    difference and exits 1.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../UPSTREAM
. "$here/UPSTREAM"
check=0
if [ "${1:-}" = "--check" ]; then check=1; shift; fi
src="${1:-}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
if [ -z "$src" ]; then
  git clone -q --depth 1 --branch "$UPSTREAM_TAG" "$UPSTREAM_REPO" "$tmp/up"
  src="$tmp/up"
fi
got="$(git -C "$src" rev-parse "HEAD^{commit}")"
if [ "$got" != "$UPSTREAM_COMMIT" ]; then
  echo "upstream checkout is at $got, expected $UPSTREAM_COMMIT ($UPSTREAM_TAG)" >&2
  exit 2
fi
# The vendored set: package directories and the licence. go.mod and go.sum are
# maintained here (the module needs a newer Go and the gocpp requirement).
stage="$tmp/stage"
mkdir -p "$stage"
for d in example internal logging ocpp ocpp1.6 ocpp1.6_test ocpp2.0.1 ocpp2.0.1_test ocppj ws; do
  mkdir -p "$stage/$d"
  rsync -a --exclude='.git' "$src/$d/" "$stage/$d/"
done
cp "$src/LICENSE" "$stage/LICENSE"
shopt -s nullglob
for p in "$here"/patches/*.patch; do
  patch -s -p1 -d "$stage" < "$p"
done
if [ "$check" = 1 ]; then
  status=0
  while IFS= read -r f; do
    if [ ! -e "$here/$f" ]; then
      echo "missing from module: $f"; status=1
    elif ! cmp -s "$stage/$f" "$here/$f"; then
      echo "differs from pinned upstream + patches: $f"; status=1
    fi
  done < <(cd "$stage" && find . -type f | sed 's#^\./##' | sort)
  # a file upstream removed by a patch must be gone here
  for p in "$here"/patches/*.patch; do
    for f in $(grep -E '^\+\+\+ /dev/null|^--- a/' "$p" | awk '/^--- a\//{print substr($2,3)}' ); do
      if [ ! -e "$stage/$f" ] && [ -e "$here/$f" ]; then
        echo "should have been deleted by patches: $f"; status=1
      fi
    done
  done
  [ "$status" = 0 ] && echo "sync-upstream: module matches $UPSTREAM_TAG + patches"
  exit "$status"
fi
rsync -a "$stage/" "$here/"
echo "synced $UPSTREAM_TAG ($UPSTREAM_COMMIT) into $here"
