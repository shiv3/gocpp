#!/usr/bin/env bash
set -uo pipefail

GOCPP_ROOT=${GOCPP_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}
SIM_ROOT=${OCPP_CP_SIM_ROOT:-$(cd "$GOCPP_ROOT/.." && pwd)/ocpp-cp-simulator}

CSMS_BIN=${CSMS_BIN:-/tmp/csms-full-B}
CSMS_LOG=${CSMS_LOG:-/tmp/gocpp-csms-full-B.log}
SIM_LOG=${SIM_LOG:-/tmp/ocpp-cp-sim-B.log}
GOCACHE=${GOCACHE:-/tmp/gocpp-go-build-cache}

CSMS_ADDR=${CSMS_ADDR:-:18092}
OPS_ADDR=${OPS_ADDR:-:19092}
OPS_URL=${OPS_URL:-http://localhost:19092}
SIM_HTTP_PORT=${SIM_HTTP_PORT:-5192}
SIM_HTTP_URL=${SIM_HTTP_URL:-http://localhost:5192}
WS_URL=${WS_URL:-ws://localhost:18092/ocpp/}

CP_ID=${CP_ID:-SC_B_001}
CONNECTORS=${CONNECTORS:-2}
SIM_RUNNER=${SIM_RUNNER:-bun}
SIM_ENTRY=${SIM_ENTRY:-src/cli/main.ts}

FAILS=0
PASSES=0
RESULTS=()
CSMS_PID=""
SIM_PID=""

sim_cli() {
  cd "$SIM_ROOT" && "$SIM_RUNNER" "$SIM_ENTRY" "$@"
}

cleanup() {
  sim_cli --stop --http-url "$SIM_HTTP_URL" >/dev/null 2>&1 || true
  if [[ -n "$SIM_PID" ]]; then
    kill "$SIM_PID" >/dev/null 2>&1 || true
    wait "$SIM_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$CSMS_PID" ]]; then
    kill "$CSMS_PID" >/dev/null 2>&1 || true
    wait "$CSMS_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT INT TERM

die() {
  printf 'fatal: %s\n' "$*" >&2
  exit 1
}

record_pass() {
  PASSES=$((PASSES + 1))
  RESULTS+=("PASS $1")
  printf 'PASS %s\n' "$1"
}

record_fail() {
  FAILS=$((FAILS + 1))
  RESULTS+=("FAIL $1 - $2")
  printf 'FAIL %s - %s\n' "$1" "$2"
}

json_eval() {
  local json=$1
  local expr=$2
  JSON_INPUT=$json JSON_EXPR=$expr bun -e '
const input = Bun.env.JSON_INPUT ?? "";
const expr = Bun.env.JSON_EXPR ?? "";
let obj;
try {
  obj = JSON.parse(input);
} catch (err) {
  console.error(`invalid JSON: ${err}`);
  process.exit(2);
}
let value;
try {
  value = new Function("o", expr)(obj);
} catch (err) {
  console.error(`json expression failed: ${err}`);
  process.exit(3);
}
if (value === undefined || value === null) process.exit(0);
if (typeof value === "object") {
  process.stdout.write(JSON.stringify(value));
} else {
  process.stdout.write(String(value));
}
'
}

wait_http() {
  local name=$1
  local url=$2
  local i
  for i in {1..75}; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.2
  done
  printf '%s did not become ready at %s\n' "$name" "$url" >&2
  return 1
}

log_count() {
  local action=$1
  if [[ ! -f "$CSMS_LOG" ]]; then
    printf '0'
    return 0
  fi
  awk \
    -v cp="\"cp\":\"$CP_ID\"" \
    -v msg="\"msg\":\"$action\"" \
    'index($0, cp) && index($0, msg) { n++ } END { print n + 0 }' \
    "$CSMS_LOG"
}

wait_log_increase() {
  local action=$1
  local before=$2
  local i current
  for i in {1..75}; do
    current=$(log_count "$action")
    if [[ "$current" -gt "$before" ]]; then
      return 0
    fi
    sleep 0.2
  done
  return 1
}

sim_send() {
  local payload=$1
  sim_cli --send "$payload" --cp-id "$CP_ID" --http-url "$SIM_HTTP_URL"
}

require_sim_send() {
  local name=$1
  local payload=$2
  local resp ok
  if ! resp=$(sim_send "$payload" 2>&1); then
    die "$name failed: $resp"
  fi
  ok=$(json_eval "$resp" 'return o.ok === true ? "true" : "false"')
  [[ "$ok" == "true" ]] || die "$name returned failure: $resp"
}

admin_call() {
  local action=$1
  local body=$2
  local tmp code rc
  tmp=$(mktemp "${TMPDIR:-/tmp}/gocpp-admin.XXXXXX") || return 1
  code=$(curl -sS -o "$tmp" -w "%{http_code}" \
    -X POST "$OPS_URL/admin/call?cp=$CP_ID&action=$action" \
    -d "$body")
  rc=$?
  ADMIN_BODY=$(cat "$tmp")
  rm -f "$tmp"
  if [[ $rc -ne 0 ]]; then
    ADMIN_ERROR="curl rc=$rc body=$ADMIN_BODY"
    return 1
  fi
  if [[ ! "$code" =~ ^2[0-9][0-9]$ ]]; then
    ADMIN_ERROR="HTTP $code body=$ADMIN_BODY"
    return 1
  fi
  printf '%s' "$ADMIN_BODY"
}

expect_admin_status() {
  local name=$1
  local action=$2
  local body=$3
  local want=$4
  local resp got
  if ! resp=$(admin_call "$action" "$body"); then
    record_fail "$name" "$ADMIN_ERROR"
    return
  fi
  got=$(json_eval "$resp" 'return o.status ?? ""')
  if [[ "$got" == "$want" ]]; then
    record_pass "$name"
  else
    record_fail "$name" "expected status=$want got=${got:-<empty>} response=$resp"
  fi
}

expect_admin_predicate() {
  local name=$1
  local action=$2
  local body=$3
  local predicate=$4
  local resp got
  if ! resp=$(admin_call "$action" "$body"); then
    record_fail "$name" "$ADMIN_ERROR"
    return
  fi
  got=$(json_eval "$resp" "$predicate")
  if [[ "$got" == "true" ]]; then
    record_pass "$name"
  else
    record_fail "$name" "predicate failed response=$resp"
  fi
}

expect_trigger_and_log() {
  local requested=$1
  local connector_json=$2
  local expected_log=$3
  local name="TriggerMessage ${requested}"
  local before resp status body
  before=$(log_count "$expected_log")
  body="{\"requestedMessage\":\"$requested\"$connector_json}"
  if ! resp=$(admin_call TriggerMessage "$body"); then
    record_fail "$name" "$ADMIN_ERROR"
    return
  fi
  status=$(json_eval "$resp" 'return o.status ?? ""')
  if [[ "$status" != "Accepted" ]]; then
    record_fail "$name" "expected status=Accepted got=${status:-<empty>} response=$resp"
    return
  fi
  if wait_log_increase "$expected_log" "$before"; then
    record_pass "$name"
  else
    record_fail "$name" "CSMS log did not receive $expected_log after trigger"
  fi
}

printf 'Building CSMS: %s\n' "$CSMS_BIN"
mkdir -p "$GOCACHE" || die "could not create GOCACHE=$GOCACHE"
export GOCACHE
go build -o "$CSMS_BIN" "$GOCPP_ROOT/examples/csms-full" || die "go build failed"

rm -f "$CSMS_LOG" "$SIM_LOG"

printf 'Starting CSMS on ADDR=%s OPS_ADDR=%s\n' "$CSMS_ADDR" "$OPS_ADDR"
ADDR=$CSMS_ADDR OPS_ADDR=$OPS_ADDR AUTO_REMOTE_START=false "$CSMS_BIN" >"$CSMS_LOG" 2>&1 &
CSMS_PID=$!
wait_http "CSMS" "$OPS_URL/healthz" || {
  sed -n '1,120p' "$CSMS_LOG" >&2
  die "CSMS health check failed"
}

printf 'Starting simulator cp=%s connectors=%s http=%s\n' "$CP_ID" "$CONNECTORS" "$SIM_HTTP_PORT"
(cd "$SIM_ROOT" && "$SIM_RUNNER" "$SIM_ENTRY" \
  --daemon \
  --http-port "$SIM_HTTP_PORT" \
  --cp-id "$CP_ID" \
  --connectors "$CONNECTORS" \
  --ws-url "$WS_URL" >"$SIM_LOG" 2>&1) &
SIM_PID=$!

wait_http "simulator" "$SIM_HTTP_URL/v1/healthz" || {
  sed -n '1,160p' "$SIM_LOG" >&2
  die "simulator health check failed"
}

if ! wait_log_increase BootNotification 0; then
  sed -n '1,160p' "$CSMS_LOG" >&2
  sed -n '1,160p' "$SIM_LOG" >&2
  die "CP did not boot against CSMS"
fi
sleep 1

printf 'Preparing active transaction on connector 1\n'
require_sim_send "authorize" '{"command":"authorize","params":{"tagId":"TAG-SC"}}'
require_sim_send "status Preparing" '{"command":"update_connector_status","params":{"connector":1,"status":"Preparing"}}'
start_before=$(log_count StartTransaction)
require_sim_send "start transaction" '{"command":"start_transaction","params":{"connector":1,"tagId":"TAG-SC"}}'
wait_log_increase StartTransaction "$start_before" || die "CSMS did not receive StartTransaction"
require_sim_send "status Charging" '{"command":"update_connector_status","params":{"connector":1,"status":"Charging"}}'
require_sim_send "set meter value" '{"command":"set_meter_value","params":{"connector":1,"value":1500}}'

status_resp=$(sim_send '{"command":"status","params":{}}') || die "status command failed"
tx_id=$(json_eval "$status_resp" 'const c = o.data?.connectors?.find((x) => x.id === 1); return c?.transactionId ?? "";')
[[ -n "$tx_id" ]] || die "could not read active transaction id from simulator status: $status_resp"

expiry=$(bun -e 'process.stdout.write(new Date(Date.now() + 3600000).toISOString())')

tx_profile=$(printf '{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1101,"transactionId":%s,"stackLevel":1,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Relative","chargingSchedule":{"duration":3600,"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":7000},{"startPeriod":1800,"limit":5000}]}}}' "$tx_id")
tx_default_profile='{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1201,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Relative","chargingSchedule":{"duration":3600,"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":6000}]}}}'
invalid_tx_profile='{"connectorId":0,"csChargingProfiles":{"chargingProfileId":1199,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Relative","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":1000}]}}}'
reserve_accepted=$(printf '{"connectorId":2,"expiryDate":"%s","idTag":"TAG-RES","reservationId":2101}' "$expiry")
reserve_occupied=$(printf '{"connectorId":1,"expiryDate":"%s","idTag":"TAG-OCC","reservationId":2102}' "$expiry")

printf '\nSmart Charging\n'
expect_admin_status "SetChargingProfile TxProfile active transaction" SetChargingProfile "$tx_profile" Accepted
expect_admin_predicate "GetCompositeSchedule active connector" GetCompositeSchedule '{"connectorId":1,"duration":3600,"chargingRateUnit":"W"}' 'return o.status === "Accepted" && o.connectorId === 1 && Array.isArray(o.chargingSchedule?.chargingSchedulePeriod);'
expect_admin_status "ClearChargingProfile by id" ClearChargingProfile '{"id":1101,"connectorId":1}' Accepted
expect_admin_status "SetChargingProfile TxDefaultProfile" SetChargingProfile "$tx_default_profile" Accepted
expect_admin_status "ClearChargingProfile by filter" ClearChargingProfile '{"connectorId":1,"chargingProfilePurpose":"TxDefaultProfile"}' Accepted
expect_admin_status "SetChargingProfile invalid TxProfile connector 0" SetChargingProfile "$invalid_tx_profile" Rejected
expect_admin_status "GetCompositeSchedule unknown connector" GetCompositeSchedule '{"connectorId":99,"duration":300,"chargingRateUnit":"W"}' Rejected

printf '\nReservation\n'
expect_admin_status "ReserveNow Accepted" ReserveNow "$reserve_accepted" Accepted
expect_admin_status "ReserveNow Occupied" ReserveNow "$reserve_occupied" Occupied
expect_admin_status "CancelReservation Accepted" CancelReservation '{"reservationId":2101}' Accepted
expect_admin_status "CancelReservation unknown id" CancelReservation '{"reservationId":2999}' Rejected

printf '\nRemote Trigger\n'
expect_trigger_and_log BootNotification '' BootNotification
expect_trigger_and_log Heartbeat '' Heartbeat
expect_trigger_and_log StatusNotification ',"connectorId":1' StatusNotification
expect_trigger_and_log MeterValues ',"connectorId":1' MeterValues
expect_admin_status "TriggerMessage invalid requestedMessage" TriggerMessage '{"requestedMessage":"StartTransaction"}' NotImplemented

printf '\nSummary\n'
for line in "${RESULTS[@]}"; do
  printf '%s\n' "$line"
done
printf 'Passed: %d  Failed: %d\n' "$PASSES" "$FAILS"

if [[ "$FAILS" -gt 0 ]]; then
  exit 1
fi
exit 0
