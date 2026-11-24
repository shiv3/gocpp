#!/usr/bin/env bash
# One command per repro:  ./run.sh r01            (upstream ocpp-go v0.19.0)
#                         ./run.sh r01 compat     (same source on the gocpp drop-in: one replace line, go.compat.mod)
#                         ./run.sh r05 compat-safe (drop-in with its opt-in safer settings: last-wins, 1 MiB limit)
#                         ./run.sh r07 native     (gocpp native API; separate module in native/)
#                         ./run.sh all | ./run.sh all compat
# Output is printed and stored in out/<name>.<variant>.txt.
set -u
cd "$(dirname "$0")"
name="${1:?usage: run.sh <rNN|all> [compat] [-- program flags]}"; variant="${2:-upstream}"
shift; [ $# -gt 0 ] && shift; [ "${1:-}" = "--" ] && shift
if [ "$name" = all ]; then
  for d in r[0-9][0-9]_*/; do ./run.sh "${d:0:3}" "$variant"; done; exit 0
fi
dir=$(ls -d ${name}_*/ 2>/dev/null | head -1); dir=${dir%/}
[ -n "$dir" ] || { echo "no such repro: $name"; exit 2; }
if [ "$variant" = native ]; then
  [ -d "native/$name" ] || { echo "no native variant for $name (only r07)"; exit 2; }
  { echo "\$ ./run.sh $name native   ($(date -u +%Y-%m-%dT%H:%M:%SZ))"; (cd native && go run "./$name"); echo "exit status: $?"; } 2>&1 | tee "out/${dir}.native.txt"; exit 0
fi
flags=(); case "$variant" in compat) flags=(-modfile=go.compat.mod -tags compat);; compat-safe) flags=(-modfile=go.compat.mod -tags compat); export REPRO_SAFE=1;; esac
out="out/${dir}.${variant}.txt"
{
  echo "\$ ./run.sh $name $variant $*   ($(date -u +%Y-%m-%dT%H:%M:%SZ), $(go version | cut -d' ' -f3,4))"
  if [ -f "$dir/cmd.sh" ]; then
    VARIANT="$variant" GOFLAGS_REPRO="${flags[*]}" bash "$dir/cmd.sh" "$@"
  else
    go run "${flags[@]}" "./$dir" "$@"
  fi
  echo "exit status: $?"
} 2>&1 | tee "$out"
