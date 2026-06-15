#!/usr/bin/env bash
set -u -o pipefail

GOCPP_REPO=${GOCPP_REPO:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)}
SIM_REPO=${SIM_REPO:-$(cd "$GOCPP_REPO/.." && pwd)/ocpp-cp-simulator}
SIM_CMD=${SIM_CMD:-"bun src/cli/main.ts"}

CP_ID=${CP_ID:-CORE_01}
CSMS_BIN=${CSMS_BIN:-/tmp/csms-full-A}
CSMS_LOG=${CSMS_LOG:-/tmp/csms-full-A.log}
SIM_LOG=${SIM_LOG:-/tmp/sim-5191.log}
GOCACHE=${GOCACHE:-/tmp/gocpp-interop-go-cache}
CSMS_ADDR=${CSMS_ADDR:-:18091}
OPS_ADDR_VALUE=${OPS_ADDR_VALUE:-:19091}
SIM_HTTP=${SIM_HTTP:-http://localhost:5191}
CSMS_ADMIN=${CSMS_ADMIN:-http://localhost:19091}
CSMS_WS_BASE=${CSMS_WS_BASE:-ws://localhost:18091/ocpp/}

declare -a SIM_CMD_ARR
read -r -a SIM_CMD_ARR <<< "$SIM_CMD"

CSMS_PID=""
SIM_PID=""
PASS_COUNT=0
FAIL_COUNT=0
RESULT_NAMES=()
RESULT_STATUSES=()
RESULT_REASONS=()

record_result() {
  local name=$1
  local status=$2
  local reason=$3

  RESULT_NAMES+=("$name")
  RESULT_STATUSES+=("$status")
  RESULT_REASONS+=("$reason")
  if [[ $status == "PASS" ]]; then
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    FAIL_COUNT=$((FAIL_COUNT + 1))
  fi
  printf '%-42s %s - %s\n' "$name" "$status" "$reason"
}

stop_sim() {
  (cd "$SIM_REPO" && "${SIM_CMD_ARR[@]}" --stop --http-url "$SIM_HTTP") >/dev/null 2>&1 || true
  if [[ -n ${SIM_PID:-} ]]; then
    kill "$SIM_PID" >/dev/null 2>&1 || true
    wait "$SIM_PID" >/dev/null 2>&1 || true
  fi
}

cleanup() {
  stop_sim
  if [[ -n ${CSMS_PID:-} ]]; then
    kill "$CSMS_PID" >/dev/null 2>&1 || true
    wait "$CSMS_PID" >/dev/null 2>&1 || true
  fi
}
trap cleanup EXIT

require_tool() {
  local tool=$1
  if ! command -v "$tool" >/dev/null 2>&1; then
    echo "missing required tool: $tool" >&2
    exit 127
  fi
}

check_port_free() {
  local port=$1
  if command -v lsof >/dev/null 2>&1 &&
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >/dev/null 2>&1; then
    echo "port ${port} is already in use" >&2
    lsof -nP -iTCP:"$port" -sTCP:LISTEN >&2 || true
    exit 1
  fi
}

wait_http_ok() {
  local url=$1
  local attempts=${2:-80}
  local i
  for ((i = 1; i <= attempts; i++)); do
    if curl -fsS "$url" >/dev/null 2>&1; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

sim_cli() {
  (cd "$SIM_REPO" && "${SIM_CMD_ARR[@]}" "$@")
}

send_cp() {
  local payload=$1
  sim_cli --send "$payload" --cp-id "$CP_ID" --http-url "$SIM_HTTP"
}

admin_call_raw() {
  local action=$1
  local payload=$2
  curl -sS -X POST "${CSMS_ADMIN}/admin/call?cp=${CP_ID}&action=${action}" -d "$payload"
}

admin_call() {
  local name=$1
  local action=$2
  local payload=$3
  local jq_filter=$4
  local response

  response=$(admin_call_raw "$action" "$payload" 2>&1)
  if printf '%s' "$response" | jq -e "$jq_filter" >/dev/null 2>&1; then
    record_result "$name" "PASS" "$response"
    return 0
  fi
  record_result "$name" "FAIL" "$response"
  return 1
}

log_lines() {
  wc -l < "$SIM_LOG" | tr -d ' '
}

log_messages_since() {
  local mark=$1
  local start=$((mark + 1))
  tail -n +"$start" "$SIM_LOG" 2>/dev/null
}

cp_payload_for_action_since() {
  local mark=$1
  local action=$2
  local id

  while IFS= read -r id; do
    if [[ -z $id ]]; then
      continue
    fi
    log_messages_since "$mark" | jq -R -r --arg id "$id" '
      fromjson? |
      select(.type == "WebSocket" and (.message | startswith("Received: "))) |
      (.message | sub("^Received: "; "") | fromjson?) |
      select(.[0] == 3 and .[1] == $id) |
      .[2] | @json
    ' 2>/dev/null | tail -n 1
  done < <(
    log_messages_since "$mark" | jq -R -r --arg action "$action" '
      fromjson? |
      select(.type == "WebSocket" and (.message | startswith("Sent: "))) |
      (.message | sub("^Sent: "; "") | fromjson?) |
      select(.[0] == 2 and .[2] == $action) |
      .[1]
    ' 2>/dev/null
  )
}

cp_request_payload_for_action_since() {
  local mark=$1
  local action=$2
  log_messages_since "$mark" | jq -R -r --arg action "$action" '
    fromjson? |
    select(.type == "WebSocket" and (.message | startswith("Sent: "))) |
    (.message | sub("^Sent: "; "") | fromjson?) |
    select(.[0] == 2 and .[2] == $action) |
    .[3] | @json
  ' 2>/dev/null | tail -n 1
}

wait_cp_roundtrip() {
  local name=$1
  local action=$2
  local mark=$3
  local jq_filter=$4
  local attempts=${5:-60}
  local payload=""
  local i

  for ((i = 1; i <= attempts; i++)); do
    payload=$(cp_payload_for_action_since "$mark" "$action" | tail -n 1)
    if [[ -n $payload ]]; then
      if printf '%s' "$payload" | jq -e "$jq_filter" >/dev/null 2>&1; then
        record_result "$name" "PASS" "$payload"
        return 0
      fi
      record_result "$name" "FAIL" "$payload"
      return 1
    fi
    sleep 0.25
  done

  record_result "$name" "FAIL" "no ${action}.conf observed"
  return 1
}

wait_cp_request_and_roundtrip() {
  local name=$1
  local action=$2
  local mark=$3
  local request_filter=$4
  local response_filter=$5
  local attempts=${6:-60}
  local req=""
  local resp=""
  local i

  for ((i = 1; i <= attempts; i++)); do
    req=$(cp_request_payload_for_action_since "$mark" "$action")
    resp=$(cp_payload_for_action_since "$mark" "$action" | tail -n 1)
    if [[ -n $req && -n $resp ]]; then
      if printf '%s' "$req" | jq -e "$request_filter" >/dev/null 2>&1 &&
        printf '%s' "$resp" | jq -e "$response_filter" >/dev/null 2>&1; then
        record_result "$name" "PASS" "request=${req} response=${resp}"
        return 0
      fi
      record_result "$name" "FAIL" "request=${req} response=${resp}"
      return 1
    fi
    sleep 0.25
  done

  record_result "$name" "FAIL" "no ${action} round-trip observed"
  return 1
}

run_cp_case() {
  local name=$1
  local action=$2
  local command=$3
  local jq_filter=$4
  local mark
  local response

  mark=$(log_lines)
  response=$(send_cp "$command" 2>&1)
  if ! printf '%s' "$response" | jq -e '.ok == true' >/dev/null 2>&1; then
    record_result "$name" "FAIL" "sim command failed: $response"
    return 1
  fi
  wait_cp_roundtrip "$name" "$action" "$mark" "$jq_filter"
}

run_cp_request_case() {
  local name=$1
  local action=$2
  local command=$3
  local request_filter=$4
  local response_filter=$5
  local mark
  local response

  mark=$(log_lines)
  response=$(send_cp "$command" 2>&1)
  if ! printf '%s' "$response" | jq -e '.ok == true' >/dev/null 2>&1; then
    record_result "$name" "FAIL" "sim command failed: $response"
    return 1
  fi
  wait_cp_request_and_roundtrip "$name" "$action" "$mark" "$request_filter" "$response_filter"
}

write_scenario() {
  local file=$1
  local body=$2
  printf '%s\n' "$body" > "$file"
}

run_scenario_case() {
  local name=$1
  local action=$2
  local scenario_file=$3
  local request_filter=$4
  local response_filter=$5
  local mark
  local response
  local scenario_id

  mark=$(log_lines)
  response=$(send_cp "{\"command\":\"run_scenario_file\",\"params\":{\"connector\":1,\"file\":\"${scenario_file}\"}}" 2>&1)
  if ! printf '%s' "$response" | jq -e '.ok == true and (.data.scenarioId | type == "string")' >/dev/null 2>&1; then
    record_result "$name" "FAIL" "scenario command failed: $response"
    return 1
  fi
  scenario_id=$(printf '%s' "$response" | jq -r '.data.scenarioId')
  wait_cp_request_and_roundtrip "$name" "$action" "$mark" "$request_filter" "$response_filter" 80
  send_cp "{\"command\":\"scenario_status\",\"params\":{\"connector\":1,\"scenarioId\":\"${scenario_id}\"}}" >/dev/null 2>&1 || true
}

print_summary() {
  local i
  echo
  echo "Summary"
  printf '%-42s %-6s %s\n' "Case" "Result" "Reason"
  printf '%-42s %-6s %s\n' "----" "------" "------"
  for ((i = 0; i < ${#RESULT_NAMES[@]}; i++)); do
    printf '%-42s %-6s %s\n' "${RESULT_NAMES[$i]}" "${RESULT_STATUSES[$i]}" "${RESULT_REASONS[$i]}"
  done
  echo
  echo "Totals: PASS=${PASS_COUNT} FAIL=${FAIL_COUNT}"
}

require_tool go
require_tool curl
require_tool jq

check_port_free "${CSMS_ADDR#:}"
check_port_free "${OPS_ADDR_VALUE#:}"
check_port_free "${SIM_HTTP##*:}"

if [[ ${CP_ID} != CORE_* ]]; then
  echo "CP_ID must be prefixed CORE_: ${CP_ID}" >&2
  exit 2
fi

: > "$CSMS_LOG"
: > "$SIM_LOG"

echo "Building CSMS..."
mkdir -p "$GOCACHE"
if ! GOCACHE="$GOCACHE" go build -o "$CSMS_BIN" "${GOCPP_REPO}/examples/csms-full"; then
  echo "CSMS build failed" >&2
  exit 1
fi

echo "Starting CSMS..."
ADDR="$CSMS_ADDR" OPS_ADDR="$OPS_ADDR_VALUE" AUTO_REMOTE_START=false "$CSMS_BIN" >> "$CSMS_LOG" 2>&1 &
CSMS_PID=$!
if ! wait_http_ok "${CSMS_ADMIN}/healthz" 80; then
  echo "CSMS did not become healthy; see ${CSMS_LOG}" >&2
  exit 1
fi

echo "Starting simulator..."
(cd "$SIM_REPO" && "${SIM_CMD_ARR[@]}" --daemon --http-port 5191 --cp-id "$CP_ID" --connectors 2 --ws-url "$CSMS_WS_BASE" --log-format json >> "$SIM_LOG" 2>&1) &
SIM_PID=$!
if ! wait_http_ok "${SIM_HTTP}/v1/healthz" 80; then
  echo "simulator did not become healthy; see ${SIM_LOG}" >&2
  exit 1
fi

wait_cp_roundtrip "CP->CSMS BootNotification" "BootNotification" 0 '.status == "Accepted" and (.interval | type == "number") and (.currentTime | type == "string")' 80

run_cp_case "CP->CSMS Heartbeat" "Heartbeat" \
  '{"command":"heartbeat","params":{}}' \
  '.currentTime | type == "string"'

run_cp_request_case "CP->CSMS StatusNotification Available" "StatusNotification" \
  '{"command":"update_connector_status","params":{"connector":1,"status":"Available"}}' \
  '.connectorId == 1 and .status == "Available" and .errorCode == "NoError"' \
  'type == "object"'

run_cp_request_case "CP->CSMS StatusNotification Preparing" "StatusNotification" \
  '{"command":"update_connector_status","params":{"connector":1,"status":"Preparing"}}' \
  '.connectorId == 1 and .status == "Preparing" and .errorCode == "NoError"' \
  'type == "object"'

run_cp_request_case "CP->CSMS StatusNotification Charging" "StatusNotification" \
  '{"command":"update_connector_status","params":{"connector":1,"status":"Charging"}}' \
  '.connectorId == 1 and .status == "Charging" and .errorCode == "NoError"' \
  'type == "object"'

FAULT_SCENARIO=/tmp/core-status-fault.json
write_scenario "$FAULT_SCENARIO" '{"id":"core-status-fault","name":"Core Status Fault","targetType":"connector","targetId":1,"trigger":{"type":"manual"},"defaultExecutionMode":"oneshot","enabled":true,"nodes":[{"id":"start","type":"start","position":{"x":0,"y":0},"data":{"label":"Start"}},{"id":"fault","type":"statusNotification","position":{"x":0,"y":100},"data":{"label":"Faulted","status":"Faulted","errorCode":"GroundFailure","info":"interop fault","vendorErrorCode":"GF-1"}},{"id":"end","type":"end","position":{"x":0,"y":200},"data":{"label":"End"}}],"edges":[{"id":"e1","source":"start","target":"fault"},{"id":"e2","source":"fault","target":"end"}]}'
run_scenario_case "CP->CSMS StatusNotification Faulted" "StatusNotification" "$FAULT_SCENARIO" \
  '.connectorId == 1 and .status == "Faulted" and .errorCode == "GroundFailure" and .vendorErrorCode == "GF-1"' \
  'type == "object"'

run_cp_request_case "CP->CSMS Authorize valid" "Authorize" \
  '{"command":"authorize","params":{"tagId":"CORE_VALID"}}' \
  '.idTag == "CORE_VALID"' \
  '.idTagInfo.status == "Accepted"'

run_cp_request_case "CP->CSMS Authorize unknown idTag" "Authorize" \
  '{"command":"authorize","params":{"tagId":"UNKNOWN_ID"}}' \
  '.idTag == "UNKNOWN_ID"' \
  '.idTagInfo.status == "Invalid"'

admin_call "CSMS->CP ChangeConfiguration Accepted" "ChangeConfiguration" \
  '{"key":"MeterValuesSampledData","value":"Energy.Active.Import.Register,Voltage,Current.Import,Power.Active.Import"}' \
  '.status == "Accepted"'

admin_call "CSMS->CP ChangeConfiguration unsupported" "ChangeConfiguration" \
  '{"key":"UnsupportedCoreKey","value":"1"}' \
  '.status == "NotSupported" or .status == "Rejected"'

admin_call "CSMS->CP GetConfiguration specific" "GetConfiguration" \
  '{"key":["HeartbeatInterval","MeterValuesSampledData","UnsupportedCoreKey"]}' \
  '([.configurationKey[].key] | index("HeartbeatInterval") != null) and ([.configurationKey[].key] | index("MeterValuesSampledData") != null) and (.unknownKey | index("UnsupportedCoreKey") != null)'

admin_call "CSMS->CP GetConfiguration all" "GetConfiguration" \
  '{"key":[]}' \
  '(.configurationKey | type == "array") and (.configurationKey | length > 5)'

run_cp_request_case "CP->CSMS StartTransaction" "StartTransaction" \
  '{"command":"start_transaction","params":{"connector":1,"tagId":"CORE_VALID"}}' \
  '.connectorId == 1 and .idTag == "CORE_VALID" and (.meterStart | type == "number")' \
  '(.transactionId | type == "number") and .idTagInfo.status == "Accepted"'

run_cp_request_case "CP->CSMS MeterValues multi-sampled" "MeterValues" \
  '{"command":"send_meter_value","params":{"connector":1}}' \
  '.connectorId == 1 and (.meterValue[0].sampledValue | length) >= 2' \
  'type == "object"'

run_cp_request_case "CP->CSMS StopTransaction" "StopTransaction" \
  '{"command":"stop_transaction","params":{"connector":1}}' \
  '(.transactionId | type == "number") and (.meterStop | type == "number")' \
  'has("idTagInfo")'

DATA_SCENARIO=/tmp/core-data-transfer.json
write_scenario "$DATA_SCENARIO" '{"id":"core-data-transfer","name":"Core DataTransfer","targetType":"connector","targetId":1,"trigger":{"type":"manual"},"defaultExecutionMode":"oneshot","enabled":true,"nodes":[{"id":"start","type":"start","position":{"x":0,"y":0},"data":{"label":"Start"}},{"id":"data","type":"dataTransfer","position":{"x":0,"y":100},"data":{"label":"DataTransfer","vendorId":"gocpp.interop","messageId":"core","data":"ping"}},{"id":"end","type":"end","position":{"x":0,"y":200},"data":{"label":"End"}}],"edges":[{"id":"e1","source":"start","target":"data"},{"id":"e2","source":"data","target":"end"}]}'
run_scenario_case "CP->CSMS DataTransfer" "DataTransfer" "$DATA_SCENARIO" \
  '.vendorId == "gocpp.interop" and .messageId == "core" and .data == "ping"' \
  '.status == "Accepted"'

admin_call "CSMS->CP ClearCache" "ClearCache" \
  '{}' \
  '.status == "Accepted"'

admin_call "CSMS->CP UnlockConnector" "UnlockConnector" \
  '{"connectorId":1}' \
  '.status == "Unlocked" or .status == "NotSupported"'

admin_call "CSMS->CP ChangeAvailability Inoperative" "ChangeAvailability" \
  '{"connectorId":2,"type":"Inoperative"}' \
  '.status == "Accepted" or .status == "Scheduled"'

admin_call "CSMS->CP ChangeAvailability Operative" "ChangeAvailability" \
  '{"connectorId":2,"type":"Operative"}' \
  '.status == "Accepted" or .status == "Scheduled"'

admin_call "CSMS->CP RemoteStartTransaction" "RemoteStartTransaction" \
  '{"idTag":"CORE_REMOTE","connectorId":1}' \
  '.status == "Accepted"'

REMOTE_TX_ID=""
for _ in {1..60}; do
  STATUS_JSON=$(send_cp '{"command":"status","params":{}}' 2>/dev/null || true)
  REMOTE_TX_ID=$(printf '%s' "$STATUS_JSON" | jq -r '.data.connectors[]? | select(.id == 1) | .transactionId // empty' 2>/dev/null | tail -n 1)
  if [[ -n $REMOTE_TX_ID && $REMOTE_TX_ID != "null" ]]; then
    break
  fi
  sleep 0.25
done

if [[ -n $REMOTE_TX_ID && $REMOTE_TX_ID != "null" ]]; then
  admin_call "CSMS->CP RemoteStopTransaction" "RemoteStopTransaction" \
    "{\"transactionId\":${REMOTE_TX_ID}}" \
    '.status == "Accepted"'
else
  record_result "CSMS->CP RemoteStopTransaction" "FAIL" "no remote-started transaction id observed"
fi

admin_call "CSMS->CP DataTransfer" "DataTransfer" \
  '{"vendorId":"unknown.vendor","messageId":"core","data":"ping"}' \
  '.status == "UnknownVendorId" or .status == "Accepted" or .status == "Rejected" or .status == "UnknownMessageId"'

admin_call "CSMS->CP Reset Soft" "Reset" \
  '{"type":"Soft"}' \
  '.status == "Accepted"'
sleep 1

admin_call "CSMS->CP Reset Hard" "Reset" \
  '{"type":"Hard"}' \
  '.status == "Accepted"'

print_summary
if [[ $FAIL_COUNT -gt 0 ]]; then
  exit 1
fi
