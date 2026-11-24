#!/bin/sh
# Rollout load-test topology runs (docs/staged-releases.md "Rollout under load").
# 4,000 chargers, 2 gateways, backend pools default:3 and canary:2, pinned to 6 cores:
#   generator 0-1, gateways 2-3, backends 4-5 (override with LT_CPUS_*).
# Usage: run.sh OUTDIR [manual|auto-healthy|auto-error ...]
# Needs: go, python3. Check `uptime` and other load tests first: do not run beside another acceptance run.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../../.." && pwd)
out=${1:?output directory}
shift
runs=${*:-manual auto-healthy auto-error}
bin=${LT_BIN:-${TMPDIR:-/tmp}/lt-bin}
gen_cpus=${LT_CPUS_GEN:-0-1}; gw_cpus=${LT_CPUS_GW:-2-3}; be_cpus=${LT_CPUS_BE:-4-5}
mkdir -p "$bin" "$out"
(cd "$root/interop/parity/cs-gocpp" && go build -o "$bin/cs-gocpp" .)
(cd "$root/cmd/gocpp-gateway" && go build -o "$bin/gocpp-gateway" .)
(cd "$root/cmd/gocppctl" && go build -o "$bin/gocppctl" .)
(cd "$root/interop/loadtest" && go build -o "$bin/loadtest" .)

ctl="$bin/gocppctl"
apply() { # FILE
  echo "date +%s.%N; $ctl route apply $here/$1 --redis-addr \$LT_REDIS --wait 10s; date +%s.%N"
}
common="-server-bin $bin/cs-gocpp -gateway-bin $bin/gocpp-gateway -gateways 2 -server-procs 1 \
  -gw-taskset $gw_cpus -be-taskset $be_cpus -addr 127.0.0.1:9700 -pprof 127.0.0.1:6160 \
  -hb 15s -mute 0.4 -flappers 0 -dups 0 -tx-frac 0.15 -restarts 0 -ramp 60s -handled-log -min-avail-mb 1500"

for r in $runs; do
  d="$out/$r"; mkdir -p "$d"
  case $r in
  manual)
    # 90 s baseline without a policy, then 1% -> 10% -> 50% -> 100% every 2 min, then back to nothing.
    taskset -c "$gen_cpus" "$bin/loadtest" $common -backend-pools default:3,canary:2 -steps 4000:50:13m \
      -label rollout-manual -out "$d" \
      -hook "180s=$(apply policy-1.yaml)" -hook "300s=$(apply policy-10.yaml)" \
      -hook "420s=$(apply policy-50.yaml)" -hook "540s=$(apply policy-100.yaml)" \
      -hook "660s=$(apply policy-0.yaml)" ;;
  auto-healthy)
    taskset -c "$gen_cpus" "$bin/loadtest" $common -backend-pools default:3,canary:2 -steps 4000:50:10m \
      -label rollout-auto-healthy -out "$d" \
      -hook "180s=date +%s.%N; $ctl rollout run $here/plan-auto.yaml --redis-addr \$LT_REDIS --scrape \$LT_GW_METRICS; date +%s.%N" ;;
  auto-error)
    taskset -c "$gen_cpus" "$bin/loadtest" $common -backend-pools default:3,canary:2 -pool-args "canary=-fail-rate 0.3" -steps 4000:50:8m \
      -label rollout-auto-error -out "$d" \
      -hook "180s=date +%s.%N; $ctl rollout run $here/plan-auto.yaml --redis-addr \$LT_REDIS --scrape \$LT_GW_METRICS; date +%s.%N" ;;
  esac
done
