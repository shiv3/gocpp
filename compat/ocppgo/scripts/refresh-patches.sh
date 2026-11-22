#!/usr/bin/env bash
# Regenerates patches/*.patch: one unified diff per upstream file that this
# module changes (the difference between the pinned upstream file and the file
# in this directory). Files that exist only here and files listed in
# patches/DELETE are not patches.
#
#   scripts/refresh-patches.sh [UPSTREAM_CHECKOUT]
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
[ "$(git -C "$src" rev-parse "HEAD^{commit}")" = "$UPSTREAM_COMMIT" ] || { echo "upstream checkout is not $UPSTREAM_COMMIT" >&2; exit 2; }
rm -f "$here"/patches/*.patch
for d in example internal logging ocpp ocpp1.6 ocpp1.6_test ocpp2.0.1 ocpp2.0.1_test ocppj ws; do
  while IFS= read -r f; do
    [ -e "$here/$f" ] || continue
    if grep -qxF "$f" "$here/patches/DELETE" 2>/dev/null; then continue; fi
    if ! cmp -s "$src/$f" "$here/$f"; then
      name="$(echo "$f" | tr '/' '-').patch"
      (cd "$tmp" && rm -rf a b && mkdir -p "a/$(dirname "$f")" "b/$(dirname "$f")" \
        && cp "$src/$f" "a/$f" && cp "$here/$f" "b/$f" \
        && { diff -u "a/$f" "b/$f" || true; }) > "$here/patches/$name"
      echo "patch: $f"
    fi
  done < <(cd "$src" && find "$d" -type f | sort)
done
