#!/usr/bin/env bash
set -u

GOCPP_ROOT="${GOCPP_ROOT:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}"
SIM_DIR="${SIM_DIR:-$(cd "$GOCPP_ROOT/.." && pwd)/ocpp-cp-simulator}"
INTEROP_DIR="${INTEROP_DIR:-${GOCPP_ROOT}/examples/csms-full/interop}"

CSMS_BIN="${CSMS_BIN:-/tmp/csms-full-C}"
CSMS_LOG="${CSMS_LOG:-/tmp/csms-C.log}"
CSMS_BUILD_LOG="${CSMS_BUILD_LOG:-/tmp/csms-C-build.log}"
SIM_LOG="${SIM_LOG:-/tmp/daemon-C.log}"

ADDR="${ADDR:-:18093}"
OPS_ADDR="${OPS_ADDR:-:19093}"
SIM_HTTP_PORT="${SIM_HTTP_PORT:-5193}"
CP_ID="${CP_ID:-FW_CP1}"
CONNECTORS="${CONNECTORS:-1}"
GOCACHE="${GOCACHE:-/tmp/go-build-csms-full-C}"

CSMS_PID=""
SIM_PID=""
RESULTS=""
PASS_COUNT=0
FAIL_COUNT=0

CASES="
GetDiagnostics
DiagnosticsStatusNotification
UpdateFirmware
FirmwareStatusNotification
GetLocalListVersion
SendLocalList Full
SendLocalList Differential
"

mkdir -p "$INTEROP_DIR"

hostport_from_addr() {
  case "$1" in
    :*) printf "localhost%s" "$1" ;;
    0.0.0.0:*) printf "localhost:%s" "${1##*:}" ;;
    "[::]:"*) printf "localhost:%s" "${1##*:}" ;;
    *) printf "%s" "$1" ;;
  esac
}

OPS_HOSTPORT="$(hostport_from_addr "$OPS_ADDR")"
WS_HOSTPORT="$(hostport_from_addr "$ADDR")"
OPS_BASE="http://${OPS_HOSTPORT}"
SIM_BASE="http://localhost:${SIM_HTTP_PORT}"
WS_URL="${WS_URL:-ws://${WS_HOSTPORT}/ocpp/}"

cleanup() {
  (cd "$SIM_DIR" && bun src/cli/main.ts --stop --http-url "$SIM_BASE" >/tmp/csms-C-sim-stop.out 2>/tmp/csms-C-sim-stop.err) || true
  if [ -n "$SIM_PID" ]; then
    kill "$SIM_PID" >/dev/null 2>&1 || true
    wait "$SIM_PID" >/dev/null 2>&1 || true
  fi
  if [ -n "$CSMS_PID" ]; then
    kill "$CSMS_PID" >/dev/null 2>&1 || true
    wait "$CSMS_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

record() {
  name="$1"
  status="$2"
  detail="$3"
  printf '[%s] %s - %s\n' "$status" "$name" "$detail"
  RESULTS="${RESULTS}${status}|${name}|${detail}"$'\n'
  if [ "$status" = "PASS" ]; then
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
}

print_summary() {
  printf '\n%-36s %-6s %s\n' "Case" "Result" "Detail"
  printf '%-36s %-6s %s\n' "----" "------" "------"
  printf '%s' "$RESULTS" | while IFS='|' read -r status name detail; do
    [ -n "$status" ] || continue
    printf '%-36s %-6s %s\n' "$name" "$status" "$detail"
  done
  printf '\nSummary: %d PASS, %d FAIL\n' "$PASS_COUNT" "$FAIL_COUNT"
}

fail_all_setup() {
  reason="$1"
  while IFS= read -r case_name; do
    [ -n "$case_name" ] || continue
    record "$case_name" "FAIL" "$reason"
  done <<EOF_CASES
$CASES
EOF_CASES
  print_summary
  exit 1
}

wait_for_http() {
  url="$1"
  attempts="${2:-50}"
  i=0
  while [ "$i" -lt "$attempts" ]; do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.2
  done
  return 1
}

wait_for_log_regex() {
  pattern="$1"
  attempts="${2:-50}"
  i=0
  while [ "$i" -lt "$attempts" ]; do
    if grep -Eq "$pattern" "$CSMS_LOG"; then
      return 0
    fi
    i=$((i + 1))
    sleep 0.2
  done
  return 1
}

admin_call() {
  action="$1"
  body="$2"
  ADMIN_RAW="$(curl -sS -w '\n__HTTP_STATUS__:%{http_code}' \
    -H 'Content-Type: application/json' \
    -X POST "${OPS_BASE}/admin/call?cp=${CP_ID}&action=${action}" \
    -d "$body" 2>&1)"
  ADMIN_CODE="${ADMIN_RAW##*__HTTP_STATUS__:}"
  ADMIN_BODY="${ADMIN_RAW%$'\n'__HTTP_STATUS__:*}"
  case "$ADMIN_CODE" in
    2*) return 0 ;;
    *) return 1 ;;
  esac
}

send_cp() {
  payload="$1"
  SEND_RAW="$(cd "$SIM_DIR" && bun src/cli/main.ts --send "$payload" --cp-id "$CP_ID" --http-url "$SIM_BASE" 2>&1)"
  case "$SEND_RAW" in
    *'"ok":true'*) return 0 ;;
    *) return 1 ;;
  esac
}

printf 'Building CSMS: %s\n' "$CSMS_BIN"
if ! GOCACHE="$GOCACHE" go build -o "$CSMS_BIN" "${GOCPP_ROOT}/examples/csms-full" >"$CSMS_BUILD_LOG" 2>&1; then
  fail_all_setup "CSMS build failed; see ${CSMS_BUILD_LOG}"
fi

: >"$CSMS_LOG"
: >"$SIM_LOG"

printf 'Starting CSMS on ADDR=%s OPS_ADDR=%s\n' "$ADDR" "$OPS_ADDR"
ADDR="$ADDR" OPS_ADDR="$OPS_ADDR" AUTO_REMOTE_START=false "$CSMS_BIN" >"$CSMS_LOG" 2>&1 &
CSMS_PID=$!
if ! wait_for_http "${OPS_BASE}/healthz" 50; then
  fail_all_setup "CSMS health unavailable at ${OPS_BASE}/healthz; see ${CSMS_LOG}"
fi

printf 'Starting simulator daemon for cp=%s on %s\n' "$CP_ID" "$SIM_BASE"
(cd "$SIM_DIR" && bun src/cli/main.ts --daemon --unix-socket none --http-port "$SIM_HTTP_PORT" --cp-id "$CP_ID" --connectors "$CONNECTORS" --ws-url "$WS_URL" >"$SIM_LOG" 2>&1) &
SIM_PID=$!
if ! wait_for_http "${SIM_BASE}/v1/healthz" 50; then
  fail_all_setup "sim daemon unavailable at ${SIM_BASE}; see ${SIM_LOG}"
fi

if ! wait_for_log_regex "\"msg\":\"BootNotification\".*\"cp\":\"${CP_ID}\"" 75; then
  fail_all_setup "charge point did not boot against CSMS; see ${CSMS_LOG} and ${SIM_LOG}"
fi

if admin_call "GetDiagnostics" "{\"location\":\"${OPS_BASE}/healthz\",\"retries\":1,\"retryInterval\":1}" &&
  printf '%s' "$ADMIN_BODY" | grep -q '"fileName"'; then
  if wait_for_log_regex "\"msg\":\"DiagnosticsStatusNotification\".*\"cp\":\"${CP_ID}\".*\"status\":\"Uploading\"" 50; then
    record "GetDiagnostics" "PASS" "response=${ADMIN_BODY}; side-effect Uploading observed"
  else
    record "GetDiagnostics" "FAIL" "response=${ADMIN_BODY}; DiagnosticsStatusNotification side-effect not observed"
  fi
else
  record "GetDiagnostics" "FAIL" "admin HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
fi

if send_cp '{"command":"diagnostics_status_notification","params":{"status":"UploadFailed"}}'; then
  if wait_for_log_regex "\"msg\":\"DiagnosticsStatusNotification\".*\"cp\":\"${CP_ID}\".*\"status\":\"UploadFailed\"" 50; then
    record "DiagnosticsStatusNotification" "PASS" "manual --send UploadFailed observed"
  else
    record "DiagnosticsStatusNotification" "FAIL" "manual --send accepted but CSMS log did not show UploadFailed"
  fi
else
  send_error="$SEND_RAW"
  if admin_call "TriggerMessage" '{"requestedMessage":"DiagnosticsStatusNotification"}' &&
    printf '%s' "$ADMIN_BODY" | grep -q '"status":"Accepted"' &&
    wait_for_log_regex "\"msg\":\"DiagnosticsStatusNotification\".*\"cp\":\"${CP_ID}\".*\"status\":\"Idle\"" 50; then
    record "DiagnosticsStatusNotification" "PASS" "observed via TriggerMessage fallback; direct --send failed: ${send_error}"
  else
    record "DiagnosticsStatusNotification" "FAIL" "direct --send failed: ${send_error}; TriggerMessage HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
  fi
fi

retrieve_date="$(date -u +"%Y-%m-%dT%H:%M:%SZ")"
if admin_call "UpdateFirmware" "{\"location\":\"http://localhost:${SIM_HTTP_PORT}/firmware.bin\",\"retrieveDate\":\"${retrieve_date}\",\"retries\":1,\"retryInterval\":1}"; then
  if wait_for_log_regex "\"msg\":\"FirmwareStatusNotification\".*\"cp\":\"${CP_ID}\".*\"status\":\"Downloading\"" 50; then
    record "UpdateFirmware" "PASS" "response=${ADMIN_BODY}; side-effect Downloading observed"
  else
    record "UpdateFirmware" "FAIL" "response=${ADMIN_BODY}; FirmwareStatusNotification side-effect not observed"
  fi
else
  record "UpdateFirmware" "FAIL" "admin HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
fi

if send_cp '{"command":"firmware_status_notification","params":{"status":"InstallationFailed"}}'; then
  if wait_for_log_regex "\"msg\":\"FirmwareStatusNotification\".*\"cp\":\"${CP_ID}\".*\"status\":\"InstallationFailed\"" 50; then
    record "FirmwareStatusNotification" "PASS" "manual --send InstallationFailed observed"
  else
    record "FirmwareStatusNotification" "FAIL" "manual --send accepted but CSMS log did not show InstallationFailed"
  fi
else
  send_error="$SEND_RAW"
  if admin_call "TriggerMessage" '{"requestedMessage":"FirmwareStatusNotification"}' &&
    printf '%s' "$ADMIN_BODY" | grep -q '"status":"Accepted"' &&
    wait_for_log_regex "\"msg\":\"FirmwareStatusNotification\".*\"cp\":\"${CP_ID}\".*\"status\":\"Idle\"" 50; then
    record "FirmwareStatusNotification" "PASS" "observed via TriggerMessage fallback; direct --send failed: ${send_error}"
  else
    record "FirmwareStatusNotification" "FAIL" "direct --send failed: ${send_error}; TriggerMessage HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
  fi
fi

if admin_call "GetLocalListVersion" '{}' &&
  printf '%s' "$ADMIN_BODY" | grep -q '"listVersion"'; then
  record "GetLocalListVersion" "PASS" "response=${ADMIN_BODY}"
else
  record "GetLocalListVersion" "FAIL" "admin HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
fi

full_body='{"listVersion":1,"updateType":"Full","localAuthorizationList":[{"idTag":"TAG-FW-1","idTagInfo":{"status":"Accepted"}}]}'
if admin_call "SendLocalList" "$full_body" &&
  printf '%s' "$ADMIN_BODY" | grep -q '"status":"Accepted"'; then
  record "SendLocalList Full" "PASS" "response=${ADMIN_BODY}"
else
  record "SendLocalList Full" "FAIL" "admin HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
fi

diff_body='{"listVersion":2,"updateType":"Differential","localAuthorizationList":[{"idTag":"TAG-FW-2","idTagInfo":{"status":"Accepted"}}]}'
if admin_call "SendLocalList" "$diff_body" &&
  printf '%s' "$ADMIN_BODY" | grep -q '"status":"Accepted"'; then
  record "SendLocalList Differential" "PASS" "response=${ADMIN_BODY}"
else
  record "SendLocalList Differential" "FAIL" "admin HTTP ${ADMIN_CODE}: ${ADMIN_BODY}"
fi

print_summary
[ "$FAIL_COUNT" -eq 0 ]
