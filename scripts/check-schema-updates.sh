#!/usr/bin/env bash
# Compares the vendored OCPP JSON schemas (schemas/v16, v201, v21) with the
# published ones, fetched the same way `make schemas-fetch` does. Prints a report
# and exits
#   0  nothing differs
#   1  schemas were added, removed or changed upstream
#   3  no source configured (none of V16_SCHEMA_URL, V201_SCHEMA_URL, V21_SCHEMA_URL
#      set), so nothing was compared
# It never touches the working tree: the committed schemas change only through a
# reviewed `make schemas-fetch && make codegen`.
#
# Files are compared as JSON (keys sorted), so formatting differences do not count.
# For tests, UPSTREAM_DIR=<dir with v16/, v201/, v21/> replaces the download.
set -euo pipefail

root="$(cd "$(dirname "$0")/.." && pwd)"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
report="${REPORT:-$work/report.md}"
: >"$report"

versions=()
if [ -n "${UPSTREAM_DIR:-}" ]; then
  for v in v16 v201 v21; do [ -d "$UPSTREAM_DIR/$v" ] && versions+=("$v"); done
  fetched="$UPSTREAM_DIR"
else
  [ -n "${V16_SCHEMA_URL:-}" ] && versions+=(v16)
  [ -n "${V201_SCHEMA_URL:-}" ] && versions+=(v201)
  [ -n "${V21_SCHEMA_URL:-}" ] && versions+=(v21)
  if [ "${#versions[@]}" -gt 0 ]; then
    OUT_DIR="$work/upstream" "$root/scripts/fetch-schemas.sh" >&2
  fi
  fetched="$work/upstream"
fi
if [ "${#versions[@]}" -eq 0 ]; then
  echo "No schema source configured (V16_SCHEMA_URL / V201_SCHEMA_URL / V21_SCHEMA_URL); nothing compared." | tee "$report"
  exit 3
fi

# canon FILE: the JSON with sorted keys, one value per line.
canon() { jq -S . "$1"; }

drift=0
for v in "${versions[@]}"; do
  local_dir="$root/schemas/$v"
  up_dir="$fetched/$v"
  added=(); removed=(); changed=()
  for f in "$up_dir"/*.json; do
    n="$(basename "$f")"
    if [ ! -f "$local_dir/$n" ]; then added+=("$n"); continue; fi
    if ! diff -q <(canon "$f") <(canon "$local_dir/$n") >/dev/null; then changed+=("$n"); fi
  done
  for f in "$local_dir"/*.json; do
    n="$(basename "$f")"
    [ -f "$up_dir/$n" ] || removed+=("$n")
  done
  if [ $((${#added[@]} + ${#removed[@]} + ${#changed[@]})) -eq 0 ]; then
    echo "$v: identical to the published schemas ($(ls "$up_dir"/*.json | wc -l | tr -d ' ') files)" | tee -a "$report"
    continue
  fi
  drift=1
  {
    echo "### $v"
    echo
    [ ${#added[@]} -gt 0 ] && { echo "Added upstream (${#added[@]}): ${added[*]}"; echo; }
    [ ${#removed[@]} -gt 0 ] && { echo "Not published any more (${#removed[@]}): ${removed[*]}"; echo; }
    if [ ${#changed[@]} -gt 0 ]; then
      echo "Changed (${#changed[@]}): ${changed[*]}"
      echo
      for n in "${changed[@]:0:10}"; do
        echo "<details><summary>$n</summary>"
        echo
        echo '```diff'
        diff -u <(canon "$local_dir/$n") <(canon "$up_dir/$n") | tail -n +3 | head -n 60 || true
        echo '```'
        echo "</details>"
        echo
      done
    fi
  } >>"$report"
done

if [ "$drift" = 1 ]; then
  cat "$report"
  exit 1
fi
exit 0
