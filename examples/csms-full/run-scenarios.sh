#!/usr/bin/env bash
# Start the full gocpp CSMS, drive each ocpp-cp-sim scenario against it, then stop.
# Usage: examples/csms-full/run-scenarios.sh [seconds-per-scenario]
set -uo pipefail
cd "$(dirname "$0")/../.."

SECS="${1:-12}"
WS=ws://localhost:18080/ocpp/
BIN="$(mktemp -d)/csms-full"
CLOG="$(mktemp -t csms-full.XXXX.log)"
# Export OTLP telemetry to a local opentelemetry-collector if one is running
# (see docker-compose in this dir). Unset to disable telemetry.
OTLP="${OTEL_EXPORTER_OTLP_ENDPOINT:-http://localhost:4318}"

echo "building csms-full…"
go build -o "$BIN" ./examples/csms-full || exit 1

ADDR=:18080 OPS_ADDR=:19090 OTEL_EXPORTER_OTLP_ENDPOINT="$OTLP" AUTO_REMOTE_START=true "$BIN" >"$CLOG" 2>&1 &
CSMS=$!
trap 'kill "$CSMS" 2>/dev/null' EXIT

for i in $(seq 1 40); do curl -fsS localhost:19090/healthz >/dev/null 2>&1 && break; sleep 0.25; done
echo "CSMS up (pid $CSMS); log=$CLOG"

fail=0
for sc in examples/csms-full/scenarios/*.json; do
  name="$(basename "$sc" .json)"
  cpid="CP_$(echo "$name" | tr -dc 'a-zA-Z0-9')"
  slog="$(mktemp -t "sim-$name.XXXX.log")"
  echo ""
  echo "=== scenario: $name  (cp=$cpid) ==="
  # Server mode (--http-port) runs headless without a TTY; the scenario runs on startup.
  ocpp-cp-sim --http-port 5174 --unix-socket none \
    --cp-id "$cpid" --connectors 1 --ws-url "$WS" \
    --scenario-template-file "$sc" --scenario-connector all >"$slog" 2>&1 &
  SIM=$!
  sleep "$SECS"
  kill "$SIM" 2>/dev/null; wait "$SIM" 2>/dev/null
  # Surface real sim errors but ignore the clean code 1000 close emitted on shutdown.
  if grep -iE '\[ERROR\]|level=error|exception' "$slog" | grep -qvE 'code[:=] *1000'; then
    echo "  sim reported errors -> $slog"; fail=1
  fi
  # per-cp message tally from the CSMS structured log
  for m in BootNotification StatusNotification Authorize StartTransaction MeterValues StopTransaction RemoteStartTransaction; do
    n=$(grep -c "\"$m\".*\"cp\":\"$cpid\"\|\"cp\":\"$cpid\".*\"$m\"" "$CLOG")
    [ "$n" -gt 0 ] && printf "  %-22s x%s\n" "$m" "$n"
  done
  echo "  sim log: $slog"
done

echo ""
echo "=== CSMS overall message tally ==="
grep -oE '"msg":"(BootNotification|StatusNotification|Authorize|StartTransaction|MeterValues|StopTransaction|RemoteStartTransaction)"' "$CLOG" | sort | uniq -c
echo "CSMS log: $CLOG"
exit $fail
