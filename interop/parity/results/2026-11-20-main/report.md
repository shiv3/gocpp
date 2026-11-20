# Parity report 2026-11-20

Same simulator traffic against `cs-gocpp` (this checkout, 79822d9) and `cs-ocppgo` (github.com/shiv3/ocpp-go@v0.17.2-fix.3).
Simulator: `/work/wt/ocpp-cp-sim-parity` (JSON Lines mode, `--reconnect-max-attempts 0`). Runs: 1.

Re-run: `bun run interop/parity/runner/main.ts [--only <glob>] [--runs N] [--skip-slow]` (needs `make` in `interop/parity` first).

Totals: 157 cases run, 133 same, 24 differ, 0 flaky, 3 skipped.

Compared layers: `steps` (what the simulator observed for each command or frame), `wire` (every frame in the simulator's trace, from the CP side, ids/timestamps/descriptions normalised), `simEvents` (connected/disconnected with close code), `serverEvents` (connect/disconnect cause/request/response_sent/panic), `serverAlive`. Library log/error events are not compared (they are library specific); free-text such as CallError descriptions is kept in `raw/run1/<case>/<lib>/side.json`.

## Skipped

- A-callerror-answering-csms-request: the simulator answers CSMS requests from its own engine immediately; send_raw_frame cannot race it, and there is no hook to suppress the automatic answer
- E-dataflow-authorize-branch-201: OCPP 2.0.1 only (idToken payload)
- E-dataflow-remote-start-reauthorize-201: OCPP 2.0.1 only (RequestStartTransaction)

## Flaky

none

## Group E: simulator scenarios

68 scenarios run. "same?" compares all layers (scenario report incl. the CSMS-initiated exchange it saw, control-call results, the full CP-side wire trace, server events, server alive).

- pass both: 64 (cert16-octt-strictness-probe, cert16-octt-strictness-probe+csms-sends, cert16-reservation-basic, cert16-tc001-cold-boot, cert16-tc003-charging-plugin-first, cert16-tc004-charging-id-first, cert16-tc005-ev-side-disconnect, cert16-tc010-remote-start, cert16-tc011-remote-start-stop, cert16-tc012-remote-stop, cert16-tc013-hard-reset, cert16-tc014-soft-reset, cert16-tc017-unlock-occupied, cert16-tc018-unlock-failure, cert16-tc019-get-configuration-all, cert16-tc019-get-configuration-key, cert16-tc021-change-configuration, cert16-tc023-1-authorize-invalid, cert16-tc023-2-authorize-expired, cert16-tc023-3-authorize-blocked, cert16-tc024-lock-failure, cert16-tc026-remote-start-rejected, cert16-tc028-remote-stop-rejected, cert16-tc031-unlock-unknown-connector, cert16-tc042-1-get-local-list-version-not-supported, cert16-tc042-2-get-local-list-version-empty, cert16-tc043-1-send-local-list-not-supported, cert16-tc043-3-send-local-list-failed, cert16-tc043-4-send-local-list-full, cert16-tc043-5-send-local-list-differential, cert16-tc044-1-firmware-update, cert16-tc044-2-firmware-download-failed, cert16-tc044-3-firmware-install-failed, cert16-tc045-1-get-diagnostics, cert16-tc048-1-reserve-now-faulted, cert16-tc048-2-reserve-now-occupied, cert16-tc048-3-reserve-now-unavailable, cert16-tc048-4-reserve-now-rejected, cert16-tc051-cancel-reservation, cert16-tc052-cancel-reservation-rejected, cert16-tc054-trigger-message, cert16-tc055-trigger-message-rejected, cert16-tc056-central-smart-charging-txdefault, cert16-tc057-central-smart-charging-txprofile, cert16-tc059-remote-start-with-profile, cert16-tc061-clear-cache, cert16-tc064-data-transfer, cert16-tc066-get-composite-schedule, cert16-tc067-clear-charging-profile, dataflow-authorize-branch-16, dataflow-call-error-handling, dataflow-charge-to-soc, dataflow-remote-start-reauthorize-16, essential-cp-behavior, parallel-heartbeat-session, remote-start-auto-meter, remote-start-timeout-fallback+timeout, remote-start-timeout-fallback, resilience-authorization-rejected, resilience-connector-fault-recovery, resilience-ev-disconnect-recovery, resilience-power-loss-recovery, resilience-temporary-unavailable-recovery, smart-charging)
- fail both: 1 (resilience-communication-loss-recovery)
- bounded both: 3 (full-charging-cycle, multi-status-monitor, status-triggered-actions)
- differing: 0

| scenario | gocpp result | ocpp-go result | same? |
| --- | --- | --- | --- |
| cert16-octt-strictness-probe | completed/PASS; assertions no-auto-get-configuration:passed, no-get-diagnostics:passed | completed/PASS; assertions no-auto-get-configuration:passed, no-get-diagnostics:passed | yes |
| cert16-octt-strictness-probe+csms-sends | completed/PASS; assertions no-auto-get-configuration:failed, no-get-diagnostics:failed; CSMS: GetDiagnostics CallError(NotImplemented); GetConfiguration=ok | completed/PASS; assertions no-auto-get-configuration:failed, no-get-diagnostics:failed; CSMS: GetDiagnostics CallError(NotImplemented); GetConfiguration=ok | yes |
| cert16-reservation-basic | completed/SKIPPED; CSMS: ReserveNow=Accepted; expected path ok | completed/SKIPPED; CSMS: ReserveNow=Accepted; expected path ok | yes |
| cert16-tc001-cold-boot | completed/SKIPPED | completed/SKIPPED | yes |
| cert16-tc003-charging-plugin-first | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc004-charging-id-first | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc005-ev-side-disconnect | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc010-remote-start | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; expected path ok | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; expected path ok | yes |
| cert16-tc011-remote-start-stop | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; RemoteStopTransaction=Accepted; expected path ok | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; RemoteStopTransaction=Accepted; expected path ok | yes |
| cert16-tc012-remote-stop | completed/SKIPPED; CSMS: RemoteStopTransaction=Accepted; expected path ok | completed/SKIPPED; CSMS: RemoteStopTransaction=Accepted; expected path ok | yes |
| cert16-tc013-hard-reset | completed/SKIPPED; CSMS: Reset=Accepted; expected path ok | completed/SKIPPED; CSMS: Reset=Accepted; expected path ok | yes |
| cert16-tc014-soft-reset | completed/SKIPPED; CSMS: Reset=Accepted; expected path ok | completed/SKIPPED; CSMS: Reset=Accepted; expected path ok | yes |
| cert16-tc017-unlock-occupied | completed/SKIPPED; CSMS: UnlockConnector=Unlocked | completed/SKIPPED; CSMS: UnlockConnector=Unlocked | yes |
| cert16-tc018-unlock-failure | completed/SKIPPED; CSMS: UnlockConnector=UnlockFailed | completed/SKIPPED; CSMS: UnlockConnector=UnlockFailed | yes |
| cert16-tc019-get-configuration-all | completed/SKIPPED; CSMS: GetConfiguration=ok | completed/SKIPPED; CSMS: GetConfiguration=ok | yes |
| cert16-tc019-get-configuration-key | completed/SKIPPED; CSMS: GetConfiguration=ok | completed/SKIPPED; CSMS: GetConfiguration=ok | yes |
| cert16-tc021-change-configuration | completed/SKIPPED; CSMS: ChangeConfiguration=Accepted | completed/SKIPPED; CSMS: ChangeConfiguration=Accepted | yes |
| cert16-tc023-1-authorize-invalid | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc023-2-authorize-expired | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc023-3-authorize-blocked | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc024-lock-failure | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc026-remote-start-rejected | completed/SKIPPED; CSMS: RemoteStartTransaction=Rejected; expected path ok | completed/SKIPPED; CSMS: RemoteStartTransaction=Rejected; expected path ok | yes |
| cert16-tc028-remote-stop-rejected | completed/SKIPPED; CSMS: RemoteStopTransaction=Rejected | completed/SKIPPED; CSMS: RemoteStopTransaction=Rejected | yes |
| cert16-tc031-unlock-unknown-connector | completed/SKIPPED; CSMS: UnlockConnector=NotSupported | completed/SKIPPED; CSMS: UnlockConnector=NotSupported | yes |
| cert16-tc042-1-get-local-list-version-not-supported | completed/SKIPPED; CSMS: GetLocalListVersion=ok | completed/SKIPPED; CSMS: GetLocalListVersion=ok | yes |
| cert16-tc042-2-get-local-list-version-empty | completed/SKIPPED; CSMS: GetLocalListVersion=ok | completed/SKIPPED; CSMS: GetLocalListVersion=ok | yes |
| cert16-tc043-1-send-local-list-not-supported | completed/SKIPPED; CSMS: SendLocalList=NotSupported | completed/SKIPPED; CSMS: SendLocalList=NotSupported | yes |
| cert16-tc043-3-send-local-list-failed | completed/SKIPPED; CSMS: SendLocalList=Failed | completed/SKIPPED; CSMS: SendLocalList=Failed | yes |
| cert16-tc043-4-send-local-list-full | completed/SKIPPED; CSMS: SendLocalList=Accepted | completed/SKIPPED; CSMS: SendLocalList=Accepted | yes |
| cert16-tc043-5-send-local-list-differential | completed/SKIPPED; CSMS: SendLocalList=Accepted; SendLocalList=Accepted | completed/SKIPPED; CSMS: SendLocalList=Accepted; SendLocalList=Accepted | yes |
| cert16-tc044-1-firmware-update | completed/SKIPPED; CSMS: UpdateFirmware=ok; expected path ok | completed/SKIPPED; CSMS: UpdateFirmware=ok; expected path ok | yes |
| cert16-tc044-2-firmware-download-failed | completed/SKIPPED; CSMS: UpdateFirmware=ok; expected path ok | completed/SKIPPED; CSMS: UpdateFirmware=ok; expected path ok | yes |
| cert16-tc044-3-firmware-install-failed | completed/SKIPPED; CSMS: UpdateFirmware=ok; expected path ok | completed/SKIPPED; CSMS: UpdateFirmware=ok; expected path ok | yes |
| cert16-tc045-1-get-diagnostics | completed/SKIPPED; CSMS: GetDiagnostics=ok; expected path ok | completed/SKIPPED; CSMS: GetDiagnostics=ok; expected path ok | yes |
| cert16-tc048-1-reserve-now-faulted | completed/SKIPPED; CSMS: ReserveNow=Faulted | completed/SKIPPED; CSMS: ReserveNow=Faulted | yes |
| cert16-tc048-2-reserve-now-occupied | completed/SKIPPED; CSMS: ReserveNow=Occupied | completed/SKIPPED; CSMS: ReserveNow=Occupied | yes |
| cert16-tc048-3-reserve-now-unavailable | completed/SKIPPED; CSMS: ReserveNow=Unavailable | completed/SKIPPED; CSMS: ReserveNow=Unavailable | yes |
| cert16-tc048-4-reserve-now-rejected | completed/SKIPPED; CSMS: ReserveNow=Rejected | completed/SKIPPED; CSMS: ReserveNow=Rejected | yes |
| cert16-tc051-cancel-reservation | completed/SKIPPED; CSMS: ReserveNow=Accepted; CancelReservation=Accepted | completed/SKIPPED; CSMS: ReserveNow=Accepted; CancelReservation=Accepted | yes |
| cert16-tc052-cancel-reservation-rejected | completed/SKIPPED; CSMS: CancelReservation=Rejected | completed/SKIPPED; CSMS: CancelReservation=Rejected | yes |
| cert16-tc054-trigger-message | completed/SKIPPED; CSMS: TriggerMessage=Accepted; expected path ok | completed/SKIPPED; CSMS: TriggerMessage=Accepted; expected path ok | yes |
| cert16-tc055-trigger-message-rejected | completed/SKIPPED; CSMS: TriggerMessage=Rejected | completed/SKIPPED; CSMS: TriggerMessage=Rejected | yes |
| cert16-tc056-central-smart-charging-txdefault | completed/SKIPPED; CSMS: SetChargingProfile=Accepted | completed/SKIPPED; CSMS: SetChargingProfile=Accepted | yes |
| cert16-tc057-central-smart-charging-txprofile | completed/SKIPPED; CSMS: SetChargingProfile=Accepted | completed/SKIPPED; CSMS: SetChargingProfile=Accepted | yes |
| cert16-tc059-remote-start-with-profile | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; expected path ok | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; expected path ok | yes |
| cert16-tc061-clear-cache | completed/SKIPPED; CSMS: ClearCache=Accepted | completed/SKIPPED; CSMS: ClearCache=Accepted | yes |
| cert16-tc064-data-transfer | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| cert16-tc066-get-composite-schedule | completed/SKIPPED; CSMS: SetChargingProfile=Accepted; GetCompositeSchedule=Accepted | completed/SKIPPED; CSMS: SetChargingProfile=Accepted; GetCompositeSchedule=Accepted | yes |
| cert16-tc067-clear-charging-profile | completed/SKIPPED; CSMS: SetChargingProfile=Accepted; ClearChargingProfile=Accepted | completed/SKIPPED; CSMS: SetChargingProfile=Accepted; ClearChargingProfile=Accepted | yes |
| dataflow-authorize-branch-16 | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| dataflow-call-error-handling | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| dataflow-charge-to-soc | completed/SKIPPED | completed/SKIPPED | yes |
| dataflow-remote-start-reauthorize-16 | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; expected path ok | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; expected path ok | yes |
| essential-cp-behavior | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; RemoteStopTransaction=Accepted | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted; RemoteStopTransaction=Accepted | yes |
| full-charging-cycle | completed/SKIPPED; stopped by runner at budget (node meter-auto, running) | completed/SKIPPED; stopped by runner at budget (node meter-auto, running) | yes |
| multi-status-monitor | completed/SKIPPED; stopped by runner at budget (node end-faulted, running) | completed/SKIPPED; stopped by runner at budget (node end-faulted, running) | yes |
| parallel-heartbeat-session | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted | yes |
| remote-start-auto-meter | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted | yes |
| remote-start-timeout-fallback+timeout | completed/SKIPPED; expected path ok | completed/SKIPPED; expected path ok | yes |
| remote-start-timeout-fallback | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted | completed/SKIPPED; CSMS: RemoteStartTransaction=Accepted | yes |
| resilience-authorization-rejected | completed/PASS; assertions authorize-sent:passed, authorize-blocked:passed, no-start:passed; expected path ok | completed/PASS; assertions authorize-sent:passed, authorize-blocked:passed, no-start:passed; expected path ok | yes |
| resilience-communication-loss-recovery | completed/FAIL; assertions link-probe-sent:failed | completed/FAIL; assertions link-probe-sent:failed | yes |
| resilience-connector-fault-recovery | completed/PASS; assertions fault-reported:passed, fault-code:passed, recovered:passed | completed/PASS; assertions fault-reported:passed, fault-code:passed, recovered:passed | yes |
| resilience-ev-disconnect-recovery | completed/PASS; assertions stopped-by-ev:passed, available-again:passed, stop-after-start:passed | completed/PASS; assertions stopped-by-ev:passed, available-again:passed, stop-after-start:passed | yes |
| resilience-power-loss-recovery | completed/PASS; assertions rebooted:passed, status-after-boot:passed | completed/PASS; assertions rebooted:passed, status-after-boot:passed | yes |
| resilience-temporary-unavailable-recovery | completed/PASS; assertions inoperative-received:passed, went-unavailable:passed, back-available:passed; CSMS: ChangeAvailability=Accepted; ChangeAvailability=Accepted | completed/PASS; assertions inoperative-received:passed, went-unavailable:passed, back-available:passed; CSMS: ChangeAvailability=Accepted; ChangeAvailability=Accepted | yes |
| smart-charging | completed/SKIPPED | completed/SKIPPED | yes |
| status-triggered-actions | completed/SKIPPED; stopped by runner at budget (node delay-10s, running) | completed/SKIPPED; stopped by runner at budget (node delay-10s, running) | yes |

### CSMS side per scenario

What the runner sends (through `/control/call`) and when; "on waiting for X" = `scenario_status` shows state `waiting` with that expectation.

| scenario | CSMS actions |
| --- | --- |
| cert16-octt-strictness-probe | no CSMS action |
| cert16-octt-strictness-probe+csms-sends | at node wait-for-config: send GetDiagnostics {"location":"http://127.0.0.1:<port>/diag"}, GetConfiguration {} |
| cert16-reservation-basic | on waiting for ReserveNow: send ReserveNow {"connectorId":1,"expiryDate":"<ts>","idTag":"CERT-RES01","reservationId":7} |
| cert16-tc001-cold-boot | no CSMS action |
| cert16-tc003-charging-plugin-first | no CSMS action |
| cert16-tc004-charging-id-first | no CSMS action |
| cert16-tc005-ev-side-disconnect | no CSMS action |
| cert16-tc010-remote-start | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"CERT010"} |
| cert16-tc011-remote-start-stop | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"CERT011"}; then on waiting for RemoteStopTransaction: send RemoteStopTransaction {"transactionId":1} |
| cert16-tc012-remote-stop | on waiting for RemoteStopTransaction: send RemoteStopTransaction {"transactionId":1} |
| cert16-tc013-hard-reset | on waiting for Reset: send Reset {"type":"Hard"} |
| cert16-tc014-soft-reset | on waiting for Reset: send Reset {"type":"Soft"} |
| cert16-tc017-unlock-occupied | at node delay-unlock-window: send UnlockConnector {"connectorId":1} |
| cert16-tc018-unlock-failure | at node delay-unlock-window: send UnlockConnector {"connectorId":1} |
| cert16-tc019-get-configuration-all | on waiting for GetConfiguration: send GetConfiguration {} |
| cert16-tc019-get-configuration-key | on waiting for GetConfiguration: send GetConfiguration {"key":["HeartbeatInterval"]} |
| cert16-tc021-change-configuration | on waiting for ChangeConfiguration: send ChangeConfiguration {"key":"MeterValueSampleInterval","value":"10"} |
| cert16-tc023-1-authorize-invalid | no CSMS action |
| cert16-tc023-2-authorize-expired | no CSMS action |
| cert16-tc023-3-authorize-blocked | no CSMS action |
| cert16-tc024-lock-failure | no CSMS action |
| cert16-tc026-remote-start-rejected | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"CERT026"} |
| cert16-tc028-remote-stop-rejected | on waiting for RemoteStopTransaction: send RemoteStopTransaction {"transactionId":1} |
| cert16-tc031-unlock-unknown-connector | on waiting for UnlockConnector: send UnlockConnector {"connectorId":99} |
| cert16-tc042-1-get-local-list-version-not-supported | on waiting for GetLocalListVersion: send GetLocalListVersion {} |
| cert16-tc042-2-get-local-list-version-empty | on waiting for GetLocalListVersion: send GetLocalListVersion {} |
| cert16-tc043-1-send-local-list-not-supported | on waiting for SendLocalList: send SendLocalList {"listVersion":1,"updateType":"Full","localAuthorizationList":[{"idTag":"LIST-A","idTagInfo":{"status":"Accepted","expiryDate":"<ts>"}}]} |
| cert16-tc043-3-send-local-list-failed | on waiting for SendLocalList: send SendLocalList {"listVersion":1,"updateType":"Full","localAuthorizationList":[{"idTag":"LIST-A","idTagInfo":{"status":"Accepted","expiryDate":"<ts>"}}]} |
| cert16-tc043-4-send-local-list-full | on waiting for SendLocalList: send SendLocalList {"listVersion":1,"updateType":"Full","localAuthorizationList":[{"idTag":"LIST-A","idTagInfo":{"status":"Accepted","expiryDate":"<ts>"}},{"idTag":"LIST-B","idTagInfo":{"status":"Blocked","expiryDate":"<ts>"}}]} |
| cert16-tc043-5-send-local-list-differential | on waiting for SendLocalList: send SendLocalList {"listVersion":1,"updateType":"Full","localAuthorizationList":[{"idTag":"LIST-A","idTagInfo":{"status":"Accepted","expiryDate":"<ts>"}},{"idTag":"LIST-B","idTagInfo":{"status":"Accepted","expiryDate":"<ts>"}}]}; then on waiting for SendLocalList: send SendLocalList {"listVersion":2,"updateType":"Differential","localAuthorizationList":[{"idTag":"LIST-C","idTagInfo":{"status":"Accepted","expiryDate":"<ts>"}},{"idTag":"LIST-A"}]} |
| cert16-tc044-1-firmware-update | on waiting for UpdateFirmware: send UpdateFirmware {"location":"http://127.0.0.1:<port>/fw.bin","retrieveDate":"<ts>"} |
| cert16-tc044-2-firmware-download-failed | on waiting for UpdateFirmware: send UpdateFirmware {"location":"http://127.0.0.1:<port>/fw.bin","retrieveDate":"<ts>"} |
| cert16-tc044-3-firmware-install-failed | on waiting for UpdateFirmware: send UpdateFirmware {"location":"http://127.0.0.1:<port>/fw.bin","retrieveDate":"<ts>"} |
| cert16-tc045-1-get-diagnostics | on waiting for GetDiagnostics: send GetDiagnostics {"location":"http://127.0.0.1:<port>/diag"} |
| cert16-tc048-1-reserve-now-faulted | on waiting for ReserveNow: send ReserveNow {"connectorId":1,"expiryDate":"<ts>","idTag":"CERT-RES01","reservationId":7} |
| cert16-tc048-2-reserve-now-occupied | on waiting for ReserveNow: send ReserveNow {"connectorId":1,"expiryDate":"<ts>","idTag":"CERT-RES01","reservationId":7} |
| cert16-tc048-3-reserve-now-unavailable | on waiting for ReserveNow: send ReserveNow {"connectorId":1,"expiryDate":"<ts>","idTag":"CERT-RES01","reservationId":7} |
| cert16-tc048-4-reserve-now-rejected | on waiting for ReserveNow: send ReserveNow {"connectorId":1,"expiryDate":"<ts>","idTag":"CERT-RES01","reservationId":7} |
| cert16-tc051-cancel-reservation | on waiting for ReserveNow: send ReserveNow {"connectorId":1,"expiryDate":"<ts>","idTag":"CERT-RES01","reservationId":7}; then on waiting for CancelReservation: send CancelReservation {"reservationId":7} |
| cert16-tc052-cancel-reservation-rejected | on waiting for CancelReservation: send CancelReservation {"reservationId":999} |
| cert16-tc054-trigger-message | on waiting for TriggerMessage: send TriggerMessage {"requestedMessage":"StatusNotification","connectorId":1} |
| cert16-tc055-trigger-message-rejected | on waiting for TriggerMessage: send TriggerMessage {"requestedMessage":"Heartbeat"} |
| cert16-tc056-central-smart-charging-txdefault | on waiting for SetChargingProfile: send SetChargingProfile {"connectorId":1,"csChargingProfiles":{"chargingProfileId":11,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":11000}],"duration":3600,"startSchedule":"<ts>"}}} |
| cert16-tc057-central-smart-charging-txprofile | on waiting for SetChargingProfile: send SetChargingProfile {"connectorId":1,"csChargingProfiles":{"chargingProfileId":11,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":11000}],"duration":3600,"startSchedule":"<ts>"},"transactionId":1}} |
| cert16-tc059-remote-start-with-profile | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"CERT059","chargingProfile":{"chargingProfileId":12,"stackLevel":0,"chargingProfilePurpose":"TxProfile","chargingProfileKind":"Relative","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":7000}]}}} |
| cert16-tc061-clear-cache | on waiting for ClearCache: send ClearCache {} |
| cert16-tc064-data-transfer | no CSMS action |
| cert16-tc066-get-composite-schedule | on waiting for SetChargingProfile: send SetChargingProfile {"connectorId":0,"csChargingProfiles":{"chargingProfileId":11,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":11000}],"duration":3600,"startSchedule":"<ts>"}}}; then on waiting for GetCompositeSchedule: send GetCompositeSchedule {"connectorId":1,"duration":600,"chargingRateUnit":"W"} |
| cert16-tc067-clear-charging-profile | on waiting for SetChargingProfile: send SetChargingProfile {"connectorId":0,"csChargingProfiles":{"chargingProfileId":11,"stackLevel":0,"chargingProfilePurpose":"TxDefaultProfile","chargingProfileKind":"Absolute","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"startPeriod":0,"limit":11000}],"duration":3600,"startSchedule":"<ts>"}}}; then on waiting for ClearChargingProfile: send ClearChargingProfile {"id":11} |
| dataflow-authorize-branch-16 | no CSMS action |
| dataflow-call-error-handling | no CSMS action |
| dataflow-charge-to-soc | no CSMS action |
| dataflow-remote-start-reauthorize-16 | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"DEMO-TAG-2"} |
| essential-cp-behavior | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"TAG001"}; then on waiting for RemoteStopTransaction: send RemoteStopTransaction {"transactionId":1} |
| full-charging-cycle | no CSMS action [budget 25 s] |
| multi-status-monitor | sleep 1500 ms; then sim ev_plug_in {"connector":1}; then sim start_transaction {"connector":1,"tagId":"MSM-TAG"}; then sleep 4000 ms; then sim update_connector_status {"connector":1,"status":"Faulted"} [budget 25 s] |
| parallel-heartbeat-session | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"PARALLEL-TAG"} |
| remote-start-auto-meter | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"REMOTE001"} [budget 30 s] |
| remote-start-timeout-fallback+timeout | no CSMS action [budget 50 s] |
| remote-start-timeout-fallback | on waiting for RemoteStartTransaction: send RemoteStartTransaction {"connectorId":1,"idTag":"TIMEOUT-TAG"} |
| resilience-authorization-rejected | no CSMS action |
| resilience-communication-loss-recovery | sleep 1500 ms; then sim disconnect; then sleep 1500 ms; then sim connect |
| resilience-connector-fault-recovery | no CSMS action |
| resilience-ev-disconnect-recovery | no CSMS action |
| resilience-power-loss-recovery | at node wait-down: (no call); then sleep 500 ms; then sim disconnect; then sleep 1500 ms; then sim connect |
| resilience-temporary-unavailable-recovery | on waiting for ChangeAvailability: send ChangeAvailability {"connectorId":1,"type":"Inoperative"}; then on waiting for ChangeAvailability: send ChangeAvailability {"connectorId":1,"type":"Operative"} |
| smart-charging | at node trigger-charging: (no call); then sim ev_plug_in {"connector":1}; then sim start_transaction {"connector":1,"tagId":"SC-TAG"}; then sleep 4000 ms; then sim stop_transaction {"connector":1}; then at node trigger-finish: (no call); then sim update_connector_status {"connector":1,"status":"Finishing"} [budget 25 s] |
| status-triggered-actions | no CSMS action [budget 25 s] |

## Table

| case | gocpp | ocpp-go | same? |
| --- | --- | --- | --- |
| A-not-json | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-json-object | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-empty-array | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-only-type | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-type-id | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-three-elems | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-five-elems | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-numeric-id | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-action-not-string | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-unknown-action | boot=true; frame={'reply':[4,'id1','NotImplemented','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','NotSupported','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-unknown-type | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id','MessageTypeNotSupported','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-payload-array | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-payload-null | boot=true; frame={'reply':[4,'id','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-empty-id | boot=true; frame={'reply':[3,'',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-long-id | boot=true; frame={'reply':[3,'xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-hb-extra-prop | boot=true; frame={'reply':[3,'id1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-boot-extra-prop | boot=true; frame={'reply':[3,'id1',{'currentTime':'2026-01-01T00:00:00Z','interval':300,'status':'Accepted'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'currentTime':'2026-01-01T00:00:00Z','interval':300,'status':'Accepted'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-boot-missing-vendor | boot=true; frame={'reply':[4,'id1','OccurenceConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','OccurrenceConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-status-connid-string | boot=true; frame={'reply':[4,'id1','TypeConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-status-enum-case | boot=true; frame={'reply':[3,'id1',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','GenericError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-status-enum-unknown | boot=true; frame={'reply':[4,'id1','PropertyConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','GenericError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-authorize-idtag-21 | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'}}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','PropertyConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-authorize-missing-idtag | boot=true; frame={'reply':[4,'id1','OccurenceConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','OccurrenceConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-start-ts-space | boot=true; frame={'reply':[4,'id1','PropertyConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-start-ts-offset-frac | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (serverEvents) |
| A-start-ts-no-zone | boot=true; frame={'reply':[4,'id1','PropertyConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-start-ts-nanos | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (serverEvents) |
| A-start-ts-ok | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-meter-value-number | boot=true; frame={'reply':[4,'id1','TypeConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-datatransfer-data-object | boot=true; frame={'reply':[4,'id1','TypeConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'data':{'a':1},'status':'Accepted'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-callresult-unknown-id | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-callerror-unknown-id | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-callerror-4-elems | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-big-frame-1.1MiB | boot=true; frame={'reply':null,'closed':'network'}; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1009); srv disc P-A-big-frame-1.1MiB:read_error | boot=true; frame={'reply':null,'closed':'network'}; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1009); srv disc P-A-big-frame-1.1MiB:read_error | same |
| A-big-frame-900KiB | boot=true; frame=<not recorded, see wire>; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame=<not recorded, see wire>; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-duplicate-id | boot=true; first={'reply':[3,'dup1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; second={'reply':[3,'dup1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; first={'reply':[3,'dup1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; second={'reply':[3,'dup1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-duplicate-id-slow | boot=true; first={'reply':[3,'dup2',{'status':'Accepted'}],'closed':null}; second={'reply':[3,'dup2',{'status':'Accepted'}],'closed':null} | boot=true; first={'reply':[3,'dup2',{'status':'Accepted'}],'closed':null}; second={'reply':[3,'dup2',{'status':'Accepted'}],'closed':null} | same |
| B-datatransfer-panic | boot=true; result={'error':'DataTransfer <uuid> answered with CALLERROR InternalError: internal error'}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; result={'error':'DataTransfer <uuid> dropped (socket_closed)'}; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1006); **SERVER DEAD** | DIFF (steps, wire, simEvents, serverEvents, serverAlive) |
| B-datatransfer-panic-raw | boot=true; frame={'reply':[4,'dt1','InternalError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':null,'closed':'network'}; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1006); **SERVER DEAD** | DIFF (steps, wire, simEvents, serverEvents, serverAlive) |
| B-datatransfer-error | boot=true; result={'error':'DataTransfer <uuid> answered with CALLERROR InternalError: parity: deliberate application error'}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; result={'error':'DataTransfer <uuid> answered with CALLERROR InternalError: parity: deliberate application error'}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| B-datatransfer-error-raw | boot=true; frame={'reply':[4,'dt1','InternalError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'dt1','InternalError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| B-datatransfer-reject | boot=true; result={'ok':true,'data':{'status':'Rejected'}}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; result={'ok':true,'data':{'status':'Rejected'}}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| B-datatransfer-reject-raw | boot=true; frame={'reply':[3,'dt1',{'status':'Rejected'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'dt1',{'status':'Rejected'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| B-sleep-concurrent-heartbeat | boot=true; heartbeat={'reply':[3,'hb1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; heartbeatAnsweredWhileSleeping=true; sleep={'reply':[3,'sl1',{'status':'Accepted'}],'closed':null} | boot=true; heartbeat={'reply':[3,'hb1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; heartbeatAnsweredWhileSleeping=true; sleep={'reply':[3,'sl1',{'status':'Accepted'}],'closed':null} | same |
| B-sleep-then-csms-call | boot=true; csmsCall={'ok':true,'result':{'listVersion':0}}; sleep={'reply':[3,'sl1',{'status':'Accepted'}],'closed':null} | boot=true; csmsCall={'ok':true,'result':{'listVersion':0}}; sleep={'reply':[3,'sl1',{'status':'Accepted'}],'closed':null} | same |
| C-replace-same-cpid | bootA=true; bootB=true; connections=['P-C-replace-same-cpid']; probeB={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probeA={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1008); srv disc P-C-replace-same-cpid:replaced | bootA=true; bootB=true; connections=['P-C-replace-same-cpid']; probeB={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; probeA={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1008); srv disc P-C-replace-same-cpid:replaced | same |
| C-sim-clean-disconnect | boot=true; disconnect={'ok':true}; connections=[]; sim a: disconnected(1000); srv disc P-C-sim-clean-disconnect:peer_closed | boot=true; disconnect={'ok':true}; connections=[]; sim a: disconnected(1000); srv disc P-C-sim-clean-disconnect:peer_closed | same |
| C-sim-sigkill | boot=true; noticed=true; connections=[]; srv disc P-C-sim-sigkill:peer_closed | boot=true; noticed=true; connections=[]; srv disc P-C-sim-sigkill:peer_closed | same |
| C-server-disconnect | boot=true; control={'ok':true}; connections=[]; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1000); srv disc P-C-server-disconnect:server_closed | boot=true; control={'ok':true}; connections=[]; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1000); srv disc P-C-server-disconnect:server_closed | same |
| C-shutdown-then-reconnect | bootA=true; control={'ok':true}; alive=true; connectB={'error':'Connection failed: code=1002 reason=Expected 101 status code'}; bootB=false; connections=[]; sim a: disconnected(1000); sim b: disconnected(1002); srv disc P-C-shutdown-then-reconnect:server_closed | bootA=true; control={'ok':true}; alive=true; connectB={'ok':true}; bootB=true; connections=['P-C-shutdown-then-reconnect']; sim a: disconnected(1000); srv disc P-C-shutdown-then-reconnect:server_closed | DIFF (steps, wire, simEvents, serverEvents) |
| C-call-not-connected | control={'error':'charge point not connected','kind':'not_connected','ok':false} | control={'error':'charge point not connected','kind':'not_connected','ok':false} | same |
| C-call-no-answer | control={'error':'<text>','kind':'timeout','ok':false}; cpReceived=['[2,\'<id>\',\'ClearCache\',{}]','[2,\'<id>\',\'ClearCache\',{}]']; control2={'error':'<text>','kind':'timeout','ok':false}; srv disc P-C-call-no-answer:peer_closed | control={'error':'<text>','kind':'timeout','ok':false}; cpReceived=['[2,\'<id>\',\'ClearCache\',{}]','[2,\'<id>\',\'ClearCache\',{}]']; control2={'error':'<text>','kind':'timeout','ok':false}; srv disc P-C-call-no-answer:peer_closed | same |
| C-subprotocol-none | handshake={'opened':true,'protocol':'','closeCode':1002,'error':null,'replies':[],'serverClosedFirst':true} | handshake={'opened':true,'protocol':'','closeCode':1002,'error':null,'replies':[],'serverClosedFirst':true} | same |
| C-subprotocol-wrong | handshake={'opened':true,'protocol':'','closeCode':1002,'error':null,'replies':[],'serverClosedFirst':true} | handshake={'opened':true,'protocol':'','closeCode':1002,'error':null,'replies':[],'serverClosedFirst':true} | same |
| C-subprotocol-foo-first | handshake={'opened':true,'protocol':'ocpp1.6','closeCode':1000,'error':null,'replies':[[3,'b1',{'currentTime':'2026-01-01T00:00:00Z','interval':300,'status':...; srv disc P-C-subprotocol-foo-first:peer_closed | handshake={'opened':true,'protocol':'ocpp1.6','closeCode':1000,'error':null,'replies':[[3,'b1',{'currentTime':'2026-01-01T00:00:00Z','interval':300,'status':...; srv disc P-C-subprotocol-foo-first:peer_closed | same |
| C-subprotocol-ocpp16-only | handshake={'opened':true,'protocol':'ocpp1.6','closeCode':1000,'error':null,'replies':[[3,'b1',{'currentTime':'2026-01-01T00:00:00Z','interval':300,'status':...; srv disc P-C-subprotocol-ocpp16-only:peer_closed | handshake={'opened':true,'protocol':'ocpp1.6','closeCode':1000,'error':null,'replies':[[3,'b1',{'currentTime':'2026-01-01T00:00:00Z','interval':300,'status':...; srv disc P-C-subprotocol-ocpp16-only:peer_closed | same |
| C-path-no-cpid | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | same |
| C-path-trailing-slash | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | same |
| C-path-wrong-prefix | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | same |
| C-path-extra-segment | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | same |
| C-path-root | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | handshake={'opened':false,'protocol':'','closeCode':1002,'error':'error','replies':[],'serverClosedFirst':true} | same |
| C-cpid-space | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['a b']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['a b']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | same |
| C-cpid-pct2f | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['a%2Fb']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['a%2Fb']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | same |
| C-cpid-slash | boot=false; heartbeat={'error':'A raw frame needs an open WebSocket'}; connections=[]; csmsCall={'error':'charge point not connected','kind':'not_connected','ok':false}; sim a: disconnected(1002) | boot=false; heartbeat={'error':'A raw frame needs an open WebSocket'}; connections=[]; csmsCall={'error':'charge point not connected','kind':'not_connected','ok':false}; sim a: disconnected(1002) | same |
| C-cpid-unicode | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['充電器1']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['充電器1']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | same |
| C-cpid-plus-colon | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['a+b:c']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | boot=true; heartbeat={'reply':[3,'h1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; connections=['a+b:c']; csmsCall={'ok':true,'result':{'status':'Accepted'}} | same |
| C-idle-silence-75s | boot=true; closedDuringIdle=false; connections=['P-C-idle-silence-75s']; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; closedDuringIdle=true; connections=['P-C-idle-silence-75s']; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null}; sim a: disconnected(1006); srv disc P-C-idle-silence-75s:timeout | DIFF (steps, wire, simEvents, serverEvents) |
| D-normal-flow | boot=true; status={'ok':true}; authorize={'ok':true}; start={'ok':true,'data':{'started':true}}; meter={'ok':true}; stop={'ok':true}; heartbeat={'ok':true} | boot=true; status={'ok':true}; authorize={'ok':true}; start={'ok':true,'data':{'started':true}}; meter={'ok':true}; stop={'ok':true}; heartbeat={'ok':true} | same |
| D-idtag-rules | boot=true; authorize-INVALID1={'kind':'callResult','payload':{'idTagInfo':{'status':'Invalid'}}}; authorize-BLOCKED1={'kind':'callResult','payload':{'idTagInfo':{'status':'Blocked'}}}; authorize-EXPIRED1={'kind':'callResult','payload':{'idTagInfo':{'status':'Expired'}}}; start-blocked={'kind':'callResult','payload':{'idTagInfo':{'status':'Blocked'},'transactionId':1}}; stop-no-idtag={'kind':'callResult','payload':{}} | boot=true; authorize-INVALID1={'kind':'callResult','payload':{'idTagInfo':{'status':'Invalid'}}}; authorize-BLOCKED1={'kind':'callResult','payload':{'idTagInfo':{'status':'Blocked'}}}; authorize-EXPIRED1={'kind':'callResult','payload':{'idTagInfo':{'status':'Expired'}}}; start-blocked={'kind':'callResult','payload':{'idTagInfo':{'status':'Blocked'},'transactionId':1}}; stop-no-idtag={'kind':'callResult','payload':{}} | same |
| D-datatransfer-echo | boot=true; string={'ok':true,'data':{'data':'hello','status':'Accepted'}}; object={'ok':true,'data':{'data':'{\'a\':1}','status':'Accepted'}}; none={'ok':true,'data':{'status':'Accepted'}} | boot=true; string={'ok':true,'data':{'data':'hello','status':'Accepted'}}; object={'ok':true,'data':{'data':'{\'a\':1}','status':'Accepted'}}; none={'ok':true,'data':{'status':'Accepted'}} | same |
| D-csms-RemoteStartTransaction | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-RemoteStopTransaction | boot=true; start={'ok':true,'data':{'started':true}}; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; start={'ok':true,'data':{'started':true}}; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-Reset | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-ChangeConfiguration | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-GetConfiguration | boot=true; control={'ok':true,'result':{'configurationKey':[{'key':'HeartbeatInterval','readonly':false,'value':'300'}]}} | boot=true; control={'ok':true,'result':{'configurationKey':[{'key':'HeartbeatInterval','readonly':false,'value':'300'}]}} | same |
| D-csms-ChangeAvailability | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-UnlockConnector | boot=true; control={'ok':true,'result':{'status':'Unlocked'}} | boot=true; control={'ok':true,'result':{'status':'Unlocked'}} | same |
| D-csms-ClearCache | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-DataTransfer | boot=true; control={'ok':true,'result':{'status':'UnknownVendorId'}} | boot=true; control={'ok':true,'result':{'status':'UnknownVendorId'}} | same |
| D-csms-TriggerMessage | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-ReserveNow | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-CancelReservation | boot=true; control={'ok':true,'result':{'status':'Rejected'}} | boot=true; control={'ok':true,'result':{'status':'Rejected'}} | same |
| D-csms-SetChargingProfile | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-ClearChargingProfile | boot=true; control={'ok':true,'result':{'status':'Unknown'}} | boot=true; control={'ok':true,'result':{'status':'Unknown'}} | same |
| D-csms-GetCompositeSchedule | boot=true; control={'ok':true,'result':{'chargingSchedule':{'chargingRateUnit':'W','chargingSchedulePeriod':[],'duration':60},'connectorId':1,'scheduleStart':'<now>',... | boot=true; control={'error':'<text>','kind':'timeout','ok':false} | DIFF (steps, wire) |
| D-csms-GetLocalListVersion | boot=true; control={'ok':true,'result':{'listVersion':0}} | boot=true; control={'ok':true,'result':{'listVersion':0}} | same |
| D-csms-SendLocalList | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-UpdateFirmware | boot=true; control={'ok':true,'result':{}} | boot=true; control={'ok':true,'result':{}} | same |
| D-csms-GetDiagnostics | boot=true; control={'ok':true,'result':{'fileName':'diagnostics.txt'}} | boot=true; control={'ok':true,'result':{'fileName':'diagnostics.txt'}} | same |
| E-cert16-octt-strictness-probe | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-octt-strictness-probe-c1'}}; report=completed/PASS [passed/passed] errs=0 nodes=5 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-octt-strictness-probe-c1'}}; report=completed/PASS [passed/passed] errs=0 nodes=5 csms-exchange=0 | same |
| E-cert16-octt-strictness-probe+csms-sends | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-octt-strictness-probe-c1'}}; call1:GetDiagnostics={'callError':{'code':'NotImplemented'},'ok':false}; call2:GetConfiguration={'ok':true,'result':{'configurationKey':[{'key':'AllowOfflineTxForUnknownId','readonly':false,'value':'false'},{'key':'AuthorizationCacheEnabled','...; report=completed/PASS [failed/failed] errs=0 nodes=5 csms-exchange=4; http=none | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-octt-strictness-probe-c1'}}; call1:GetDiagnostics={'callError':{'code':'NotImplemented'},'ok':false}; call2:GetConfiguration={'ok':true,'result':{'configurationKey':[{'key':'AllowOfflineTxForUnknownId','readonly':false,'value':'false'},{'key':'AuthorizationCacheEnabled','...; report=completed/PASS [failed/failed] errs=0 nodes=5 csms-exchange=4; http=none | same |
| E-cert16-reservation-basic | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-reservation-basic-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=7 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-reservation-basic:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-reservation-basic-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=7 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-reservation-basic:peer_closed | same |
| E-cert16-tc001-cold-boot | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc001-cold-boot-c1'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc001-cold-boot-c1'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=0 | same |
| E-cert16-tc003-charging-plugin-first | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc003-charging-plugin-first-c1'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc003-charging-plugin-first:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc003-charging-plugin-first-c1'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc003-charging-plugin-first:peer_closed | same |
| E-cert16-tc004-charging-id-first | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc004-charging-id-first-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc004-charging-id-first:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc004-charging-id-first-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc004-charging-id-first:peer_closed | same |
| E-cert16-tc005-ev-side-disconnect | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc005-ev-side-disconnect-c1'}}; report=completed/SKIPPED errs=0 nodes=7 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc005-ev-side-disconnect:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc005-ev-side-disconnect-c1'}}; report=completed/SKIPPED errs=0 nodes=7 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc005-ev-side-disconnect:peer_closed | same |
| E-cert16-tc010-remote-start | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc010-remote-start-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc010-remote-start:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc010-remote-start-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc010-remote-start:peer_closed | same |
| E-cert16-tc011-remote-start-stop | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc011-remote-start-stop-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; call2:RemoteStopTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=7 csms-exchange=4; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc011-remote-start-stop:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc011-remote-start-stop-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; call2:RemoteStopTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=7 csms-exchange=4; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc011-remote-start-stop:peer_closed | same |
| E-cert16-tc012-remote-stop | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc012-remote-stop-c1'}}; call1:RemoteStopTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc012-remote-stop:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc012-remote-stop-c1'}}; call1:RemoteStopTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc012-remote-stop:peer_closed | same |
| E-cert16-tc013-hard-reset | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc013-hard-reset-c1'}}; call1:Reset={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000),disconnected(1000); srv disc P-E-cert16-tc013-hard-reset:peer_closed,P-E-cert16-tc013-hard-reset:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc013-hard-reset-c1'}}; call1:Reset={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000),disconnected(1000); srv disc P-E-cert16-tc013-hard-reset:peer_closed,P-E-cert16-tc013-hard-reset:peer_closed | same |
| E-cert16-tc014-soft-reset | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc014-soft-reset-c1'}}; call1:Reset={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc014-soft-reset:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc014-soft-reset-c1'}}; call1:Reset={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc014-soft-reset:peer_closed | same |
| E-cert16-tc017-unlock-occupied | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc017-unlock-occupied-c1'}}; call1:UnlockConnector={'ok':true,'result':{'status':'Unlocked'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc017-unlock-occupied-c1'}}; call1:UnlockConnector={'ok':true,'result':{'status':'Unlocked'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=2 | same |
| E-cert16-tc018-unlock-failure | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc018-unlock-failure-c1'}}; call1:UnlockConnector={'ok':true,'result':{'status':'UnlockFailed'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc018-unlock-failure-c1'}}; call1:UnlockConnector={'ok':true,'result':{'status':'UnlockFailed'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=2 | same |
| E-cert16-tc019-get-configuration-all | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc019-get-configuration-all-c1'}}; call1:GetConfiguration={'ok':true,'result':{'configurationKey':[{'key':'AllowOfflineTxForUnknownId','readonly':false,'value':'false'},{'key':'AuthorizationCacheEnabled','...; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc019-get-configuration-all-c1'}}; call1:GetConfiguration={'ok':true,'result':{'configurationKey':[{'key':'AllowOfflineTxForUnknownId','readonly':false,'value':'false'},{'key':'AuthorizationCacheEnabled','...; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | same |
| E-cert16-tc019-get-configuration-key | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc019-get-configuration-key-c1'}}; call1:GetConfiguration={'ok':true,'result':{'configurationKey':[{'key':'HeartbeatInterval','readonly':false,'value':'300'}]}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc019-get-configuration-key-c1'}}; call1:GetConfiguration={'ok':true,'result':{'configurationKey':[{'key':'HeartbeatInterval','readonly':false,'value':'300'}]}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | same |
| E-cert16-tc021-change-configuration | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc021-change-configuration-c1'}}; call1:ChangeConfiguration={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc021-change-configuration-c1'}}; call1:ChangeConfiguration={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | same |
| E-cert16-tc023-1-authorize-invalid | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc023-1-authorize-invalid-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc023-1-authorize-invalid:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc023-1-authorize-invalid-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc023-1-authorize-invalid:peer_closed | same |
| E-cert16-tc023-2-authorize-expired | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc023-2-authorize-expired-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc023-2-authorize-expired:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc023-2-authorize-expired-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc023-2-authorize-expired:peer_closed | same |
| E-cert16-tc023-3-authorize-blocked | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc023-3-authorize-blocked-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc023-3-authorize-blocked:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc023-3-authorize-blocked-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc023-3-authorize-blocked:peer_closed | same |
| E-cert16-tc024-lock-failure | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc024-lock-failure-c1'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc024-lock-failure:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc024-lock-failure-c1'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc024-lock-failure:peer_closed | same |
| E-cert16-tc026-remote-start-rejected | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc026-remote-start-rejected-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc026-remote-start-rejected:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc026-remote-start-rejected-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc026-remote-start-rejected:peer_closed | same |
| E-cert16-tc028-remote-stop-rejected | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc028-remote-stop-rejected-c1'}}; call1:RemoteStopTransaction={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc028-remote-stop-rejected-c1'}}; call1:RemoteStopTransaction={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=2 | same |
| E-cert16-tc031-unlock-unknown-connector | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc031-unlock-unknown-connector-c1'}}; call1:UnlockConnector={'ok':true,'result':{'status':'NotSupported'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc031-unlock-unknown-connector-c1'}}; call1:UnlockConnector={'ok':true,'result':{'status':'NotSupported'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | same |
| E-cert16-tc042-1-get-local-list-version-not-supported | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc042-1-get-local-list-version-not-supported-c1'}}; call1:GetLocalListVersion={'ok':true,'result':{'listVersion':-1}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc042-1-get-local-list-version-not-supported-c1'}}; call1:GetLocalListVersion={'ok':true,'result':{'listVersion':-1}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-cert16-tc042-2-get-local-list-version-empty | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc042-2-get-local-list-version-empty-c1'}}; call1:GetLocalListVersion={'ok':true,'result':{'listVersion':0}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc042-2-get-local-list-version-empty-c1'}}; call1:GetLocalListVersion={'ok':true,'result':{'listVersion':0}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-cert16-tc043-1-send-local-list-not-supported | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-1-send-local-list-not-supported-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'NotSupported'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-1-send-local-list-not-supported-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'NotSupported'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-cert16-tc043-3-send-local-list-failed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-3-send-local-list-failed-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'Failed'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-3-send-local-list-failed-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'Failed'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2 | same |
| E-cert16-tc043-4-send-local-list-full | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-4-send-local-list-full-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-4-send-local-list-full-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-cert16-tc043-5-send-local-list-differential | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-5-send-local-list-differential-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'Accepted'}}; call2:SendLocalList={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=4 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc043-5-send-local-list-differential-c1'}}; call1:SendLocalList={'ok':true,'result':{'status':'Accepted'}}; call2:SendLocalList={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=4 | same |
| E-cert16-tc044-1-firmware-update | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc044-1-firmware-update-c1'}}; call1:UpdateFirmware={'ok':true,'result':{}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2; expect=[]; http=none; sim a: disconnected(1000); srv disc P-E-cert16-tc044-1-firmware-update:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc044-1-firmware-update-c1'}}; call1:UpdateFirmware={'ok':true,'result':{}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2; expect=[]; http=none; sim a: disconnected(1000); srv disc P-E-cert16-tc044-1-firmware-update:peer_closed | same |
| E-cert16-tc044-2-firmware-download-failed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc044-2-firmware-download-failed-c1'}}; call1:UpdateFirmware={'ok':true,'result':{}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; http=none; sim a: disconnected(1000); srv disc P-E-cert16-tc044-2-firmware-download-failed:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc044-2-firmware-download-failed-c1'}}; call1:UpdateFirmware={'ok':true,'result':{}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; http=none; sim a: disconnected(1000); srv disc P-E-cert16-tc044-2-firmware-download-failed:peer_closed | same |
| E-cert16-tc044-3-firmware-install-failed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc044-3-firmware-install-failed-c1'}}; call1:UpdateFirmware={'ok':true,'result':{}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; http=none; sim a: disconnected(1000); srv disc P-E-cert16-tc044-3-firmware-install-failed:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc044-3-firmware-install-failed-c1'}}; call1:UpdateFirmware={'ok':true,'result':{}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; http=none; sim a: disconnected(1000); srv disc P-E-cert16-tc044-3-firmware-install-failed:peer_closed | same |
| E-cert16-tc045-1-get-diagnostics | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc045-1-get-diagnostics-c1'}}; call1:GetDiagnostics={'ok':true,'result':{'fileName':'diagnostics.txt'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2; expect=[]; http=['POST /diag']; sim a: disconnected(1000); srv disc P-E-cert16-tc045-1-get-diagnostics:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc045-1-get-diagnostics-c1'}}; call1:GetDiagnostics={'ok':true,'result':{'fileName':'diagnostics.txt'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2; expect=[]; http=['POST /diag']; sim a: disconnected(1000); srv disc P-E-cert16-tc045-1-get-diagnostics:peer_closed | same |
| E-cert16-tc048-1-reserve-now-faulted | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-1-reserve-now-faulted-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Faulted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-1-reserve-now-faulted-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Faulted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2 | same |
| E-cert16-tc048-2-reserve-now-occupied | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-2-reserve-now-occupied-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Occupied'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-2-reserve-now-occupied-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Occupied'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-cert16-tc048-3-reserve-now-unavailable | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-3-reserve-now-unavailable-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Unavailable'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-3-reserve-now-unavailable-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Unavailable'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2 | same |
| E-cert16-tc048-4-reserve-now-rejected | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-4-reserve-now-rejected-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc048-4-reserve-now-rejected-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2 | same |
| E-cert16-tc051-cancel-reservation | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc051-cancel-reservation-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Accepted'}}; call2:CancelReservation={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=4 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc051-cancel-reservation-c1'}}; call1:ReserveNow={'ok':true,'result':{'status':'Accepted'}}; call2:CancelReservation={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=4 | same |
| E-cert16-tc052-cancel-reservation-rejected | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc052-cancel-reservation-rejected-c1'}}; call1:CancelReservation={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc052-cancel-reservation-rejected-c1'}}; call1:CancelReservation={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | same |
| E-cert16-tc054-trigger-message | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc054-trigger-message-c1'}}; call1:TriggerMessage={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc054-trigger-message:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc054-trigger-message-c1'}}; call1:TriggerMessage={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc054-trigger-message:peer_closed | same |
| E-cert16-tc055-trigger-message-rejected | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc055-trigger-message-rejected-c1'}}; call1:TriggerMessage={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc055-trigger-message-rejected-c1'}}; call1:TriggerMessage={'ok':true,'result':{'status':'Rejected'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=2 | same |
| E-cert16-tc056-central-smart-charging-txdefault | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc056-central-smart-charging-txdefault-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc056-central-smart-charging-txdefault-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=2 | same |
| E-cert16-tc057-central-smart-charging-txprofile | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc057-central-smart-charging-txprofile-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc057-central-smart-charging-txprofile-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=2 | same |
| E-cert16-tc059-remote-start-with-profile | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc059-remote-start-with-profile-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc059-remote-start-with-profile:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc059-remote-start-with-profile-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc059-remote-start-with-profile:peer_closed | same |
| E-cert16-tc061-clear-cache | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc061-clear-cache-c1'}}; call1:ClearCache={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc061-clear-cache-c1'}}; call1:ClearCache={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=2 | same |
| E-cert16-tc064-data-transfer | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc064-data-transfer-c1'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc064-data-transfer:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc064-data-transfer-c1'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-cert16-tc064-data-transfer:peer_closed | same |
| E-cert16-tc066-get-composite-schedule | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc066-get-composite-schedule-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; call2:GetCompositeSchedule={'ok':true,'result':{'chargingSchedule':{'chargingRateUnit':'W','chargingSchedulePeriod':[{'limit':11000,'startPeriod':0}],'duration':600},'connect...; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=4 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc066-get-composite-schedule-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; call2:GetCompositeSchedule={'ok':true,'result':{'chargingSchedule':{'chargingRateUnit':'W','chargingSchedulePeriod':[{'limit':11000,'startPeriod':0}],'duration':600},'connect...; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=4 | same |
| E-cert16-tc067-clear-charging-profile | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc067-clear-charging-profile-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; call2:ClearChargingProfile={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=4 | boot=true; start={'ok':true,'data':{'scenarioId':'cert16-tc067-clear-charging-profile-c1'}}; call1:SetChargingProfile={'ok':true,'result':{'status':'Accepted'}}; call2:ClearChargingProfile={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=4 | same |
| E-dataflow-authorize-branch-16 | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-authorize-branch-16-c1'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-dataflow-authorize-branch-16:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-authorize-branch-16-c1'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-dataflow-authorize-branch-16:peer_closed | same |
| E-dataflow-call-error-handling | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-call-error-handling-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-dataflow-call-error-handling:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-call-error-handling-c1'}}; report=completed/SKIPPED errs=0 nodes=6 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-dataflow-call-error-handling:peer_closed | same |
| E-dataflow-charge-to-soc | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-charge-to-soc-c1'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-charge-to-soc-c1'}}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0 | same |
| E-dataflow-remote-start-reauthorize-16 | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-remote-start-reauthorize-16-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=11 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-dataflow-remote-start-reauthorize-16:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'dataflow-remote-start-reauthorize-16-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=11 csms-exchange=2; expect=[]; sim a: disconnected(1000); srv disc P-E-dataflow-remote-start-reauthorize-16:peer_closed | same |
| E-essential-cp-behavior | boot=true; start={'ok':true,'data':{'scenarioId':'essential-cp-behavior-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; call2:RemoteStopTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=11 csms-exchange=4 | boot=true; start={'ok':true,'data':{'scenarioId':'essential-cp-behavior-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; call2:RemoteStopTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=11 csms-exchange=4 | same |
| E-full-charging-cycle | boot=true; start={'ok':true,'data':{'scenarioId':'full-charging-cycle-c1'}}; unfinished={'state':'running','node':'meter-auto','expecting':null}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'full-charging-cycle-c1'}}; unfinished={'state':'running','node':'meter-auto','expecting':null}; report=completed/SKIPPED errs=0 nodes=3 csms-exchange=0 | same |
| E-multi-status-monitor | boot=true; start={'ok':true,'data':{'scenarioId':'multi-status-monitor-c1'}}; sim:ev_plug_in={'ok':true}; sim:start_transaction={'ok':true,'data':{'started':true}}; sim:update_connector_status={'ok':true}; unfinished={'state':'running','node':'end-faulted','expecting':null}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'multi-status-monitor-c1'}}; sim:ev_plug_in={'ok':true}; sim:start_transaction={'ok':true,'data':{'started':true}}; sim:update_connector_status={'ok':true}; unfinished={'state':'running','node':'end-faulted','expecting':null}; report=completed/SKIPPED errs=0 nodes=9 csms-exchange=0 | same |
| E-parallel-heartbeat-session | boot=true; start={'ok':true,'data':{'scenarioId':'parallel-heartbeat-session-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=12 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'parallel-heartbeat-session-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=12 csms-exchange=2 | same |
| E-remote-start-auto-meter | boot=true; start={'ok':true,'data':{'scenarioId':'remote-start-auto-meter-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'remote-start-auto-meter-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-remote-start-timeout-fallback+timeout | boot=true; start={'ok':true,'data':{'scenarioId':'remote-start-timeout-fallback-c1'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-remote-start-timeout-fallback+timeout:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'remote-start-timeout-fallback-c1'}}; report=completed/SKIPPED errs=0 nodes=4 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-remote-start-timeout-fallback+timeout:peer_closed | same |
| E-remote-start-timeout-fallback | boot=true; start={'ok':true,'data':{'scenarioId':'remote-start-timeout-fallback-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | boot=true; start={'ok':true,'data':{'scenarioId':'remote-start-timeout-fallback-c1'}}; call1:RemoteStartTransaction={'ok':true,'result':{'status':'Accepted'}}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=2 | same |
| E-resilience-authorization-rejected | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-authorization-rejected-c1'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=8 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-resilience-authorization-rejected:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-authorization-rejected-c1'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=8 csms-exchange=0; expect=[]; sim a: disconnected(1000); srv disc P-E-resilience-authorization-rejected:peer_closed | same |
| E-resilience-communication-loss-recovery | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-communication-loss-recovery-c1'}}; sim:disconnect={'ok':true}; sim:connect={'ok':true}; report=completed/FAIL [failed] errs=0 nodes=6 csms-exchange=0; sim a: disconnected(1000); srv disc P-E-resilience-communication-loss-recovery:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-communication-loss-recovery-c1'}}; sim:disconnect={'ok':true}; sim:connect={'ok':true}; report=completed/FAIL [failed] errs=0 nodes=6 csms-exchange=0; sim a: disconnected(1000); srv disc P-E-resilience-communication-loss-recovery:peer_closed | same |
| E-resilience-connector-fault-recovery | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-connector-fault-recovery-c1'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=7 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-connector-fault-recovery-c1'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=7 csms-exchange=0 | same |
| E-resilience-ev-disconnect-recovery | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-ev-disconnect-recovery-c1'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=8 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-ev-disconnect-recovery-c1'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=8 csms-exchange=0 | same |
| E-resilience-power-loss-recovery | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-power-loss-recovery-c1'}}; sim:disconnect={'ok':true}; sim:connect={'ok':true}; report=completed/PASS [passed/passed] errs=0 nodes=7 csms-exchange=0; sim a: disconnected(1000); srv disc P-E-resilience-power-loss-recovery:peer_closed | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-power-loss-recovery-c1'}}; sim:disconnect={'ok':true}; sim:connect={'ok':true}; report=completed/PASS [passed/passed] errs=0 nodes=7 csms-exchange=0; sim a: disconnected(1000); srv disc P-E-resilience-power-loss-recovery:peer_closed | same |
| E-resilience-temporary-unavailable-recovery | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-temporary-unavailable-recovery-c1'}}; call1:ChangeAvailability={'ok':true,'result':{'status':'Accepted'}}; call2:ChangeAvailability={'ok':true,'result':{'status':'Accepted'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=6 csms-exchange=4 | boot=true; start={'ok':true,'data':{'scenarioId':'resilience-temporary-unavailable-recovery-c1'}}; call1:ChangeAvailability={'ok':true,'result':{'status':'Accepted'}}; call2:ChangeAvailability={'ok':true,'result':{'status':'Accepted'}}; report=completed/PASS [passed/passed/passed] errs=0 nodes=6 csms-exchange=4 | same |
| E-smart-charging | boot=true; start={'ok':true,'data':{'scenarioId':'smart-charging-c1'}}; sim:ev_plug_in={'ok':true}; sim:start_transaction={'ok':true,'data':{'started':true}}; sim:stop_transaction={'ok':true}; sim:update_connector_status={'ok':true}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'smart-charging-c1'}}; sim:ev_plug_in={'ok':true}; sim:start_transaction={'ok':true,'data':{'started':true}}; sim:stop_transaction={'ok':true}; sim:update_connector_status={'ok':true}; report=completed/SKIPPED errs=0 nodes=5 csms-exchange=0 | same |
| E-status-triggered-actions | boot=true; start={'ok':true,'data':{'scenarioId':'status-triggered-actions-c1'}}; unfinished={'state':'running','node':'delay-10s','expecting':null}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=0 | boot=true; start={'ok':true,'data':{'scenarioId':'status-triggered-actions-c1'}}; unfinished={'state':'running','node':'delay-10s','expecting':null}; report=completed/SKIPPED errs=0 nodes=8 csms-exchange=0 | same |

## Differences in detail

### A-type-id

[2,"id"]

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":null,"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id","FormationViolation","<desc>",{}]`

Side (gocpp): `{'texts':['call must have 4 elements','[server log] warn ocpp parse error {\'cp_id\':\'P-A-type-id\',\'err\':\'ocppj: protocol error at shape: call...`

Side (ocpp-go): `{'texts':['[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 2

### A-action-not-string

action is a number

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":null,"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id","FormationViolation","<desc>",{}]`

Side (gocpp): `{'texts':['action not a string','[server log] warn ocpp parse error {\'cp_id\':\'P-A-action-not-string\',\'err\':\'ocppj: protocol error at shape: ...`

Side (ocpp-go): `{'texts':['[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 2

### A-unknown-action

unknown action Foo

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","NotImplemented","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","NotSupported","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","NotImplemented","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","NotSupported","<desc>",{}]`

Side (gocpp): `{'texts':['action Foo not implemented']}`

Side (ocpp-go): `{'texts':['Unsupported feature Foo','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-unknown-type

unknown message type 9

Layer `steps`:

- only gocpp: `frame: {"reply":null,"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id","MessageTypeNotSupported","<desc>",{}],"closed":null}`

Layer `wire`:

- only ocpp-go: `a: <- [4,"id","MessageTypeNotSupported","<desc>",{}]`

Side (ocpp-go): `{'texts':['Invalid message type ID 9','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-payload-null

payload is null

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[3,"id",{"currentTime":"2026-01-01T00:00:00Z"}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id","FormationViolation","<desc>",{}]`
- only ocpp-go: `a: <- [3,"id",{"currentTime":"2026-01-01T00:00:00Z"}]`

Layer `serverEvents`:

- only ocpp-go: `{"kind":"request","cpId":"P-A-payload-null","action":"Heartbeat","payload":{}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-A-payload-null","action":"Heartbeat","payload":{"currentTime":"2026-01-01T00:00:00Z"}}`

Side (gocpp): `{'texts':['schema Heartbeat.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/Heartbeat.json#'\n- at '': got null, want object']}`

### A-empty-id

empty message id

Layer `steps`:

- only gocpp: `frame: {"reply":[3,"",{"currentTime":"2026-01-01T00:00:00Z"}],"closed":null}`
- only ocpp-go: `frame: {"reply":null,"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [3,"",{"currentTime":"2026-01-01T00:00:00Z"}]`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-empty-id","action":"Heartbeat","payload":{}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-A-empty-id","action":"Heartbeat","payload":{"currentTime":"2026-01-01T00:00:00Z"}}`

Side (ocpp-go): `{'texts':['[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-long-id

message id of 40 chars (spec max 36)

Layer `steps`:

- only gocpp: `frame: {"reply":[3,"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",{"currentTime":"2026-01-01T00:00:00Z"}],"closed":null}`
- only ocpp-go: `frame: {"reply":null,"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [3,"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",{"currentTime":"2026-01-01T00:00:00Z"}]`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-long-id","action":"Heartbeat","payload":{}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-A-long-id","action":"Heartbeat","payload":{"currentTime":"2026-01-01T00:00:00Z"}}`

Side (ocpp-go): `{'texts':['[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 1

### A-boot-missing-vendor

BootNotification missing chargePointVendor

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","OccurenceConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","OccurrenceConstraintViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","OccurenceConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","OccurrenceConstraintViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema BootNotification.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/BootNotification.json#'\n- at '': missing p...`

Side (ocpp-go): `{'texts':['Field Call.Payload.ChargePointVendor required but not found for feature BootNotification','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-status-connid-string

StatusNotification connectorId "1"

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","TypeConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","TypeConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","FormationViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema StatusNotification.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/StatusNotification.json#'\n- at '/connect...`

Side (ocpp-go): `{'texts':['json: cannot unmarshal string into Go struct field .connectorId of type int','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-status-enum-case

StatusNotification errorCode "noerror"

Layer `steps`:

- only gocpp: `frame: {"reply":[3,"id1",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","GenericError","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [3,"id1",{}]`
- only ocpp-go: `a: <- [4,"id1","GenericError","<desc>",{}]`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-status-enum-case","action":"StatusNotification","payload":{"connectorId":1,"errorCode":"NoError","status":"Available"}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-A-status-enum-case","action":"StatusNotification","payload":{}}`

Side (gocpp): `{'texts':['[server log] warn schema soft violations passed (lenient) {\'action\':\'StatusNotification\',\'keywords\':\'[enum]\',\'kind\':\'request\...`

Side (ocpp-go): `{'texts':['Key: 'Call.Payload.ErrorCode' Error:Field validation for 'ErrorCode' failed on the 'chargePointErrorCode' tag','[server error] error lib...`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 2

### A-status-enum-unknown

StatusNotification unknown status

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","PropertyConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","GenericError","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","PropertyConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","GenericError","<desc>",{}]`

Side (gocpp): `{'texts':['schema StatusNotification.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/StatusNotification.json#'\n- at '/status'...`

Side (ocpp-go): `{'texts':['Key: 'Call.Payload.Status' Error:Field validation for 'Status' failed on the 'chargePointStatus' tag','[server error] error library log ...`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-authorize-idtag-21

Authorize idTag 21 chars (maxLength 20)

Layer `steps`:

- only gocpp: `frame: {"reply":[3,"id1",{"idTagInfo":{"status":"Accepted"}}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","PropertyConstraintViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [3,"id1",{"idTagInfo":{"status":"Accepted"}}]`
- only ocpp-go: `a: <- [4,"id1","PropertyConstraintViolation","<desc>",{}]`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-authorize-idtag-21","action":"Authorize","payload":{"idTag":"aaaaaaaaaaaaaaaaaaaaa"}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-A-authorize-idtag-21","action":"Authorize","payload":{"idTagInfo":{"status":"Accepted"}}}`

Side (gocpp): `{'texts':['[server log] warn schema soft violations passed (lenient) {\'action\':\'Authorize\',\'keywords\':\'[maxLength]\',\'kind\':\'request\',\'...`

Side (ocpp-go): `{'texts':['Field Call.Payload.IdTag must be maximum 20, but was 21 for feature Authorize','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 2

### A-authorize-missing-idtag

Authorize missing idTag

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","OccurenceConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","OccurrenceConstraintViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","OccurenceConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","OccurrenceConstraintViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema Authorize.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/Authorize.json#'\n- at '': missing property 'idTag...`

Side (ocpp-go): `{'texts':['Field Call.Payload.IdTag required but not found for feature Authorize','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-start-ts-space

StartTransaction timestamp "2026-01-01 00:00:00"

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","PropertyConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","PropertyConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","FormationViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema StartTransaction.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/StartTransaction.json#'\n- at '/timestamp':...`

Side (ocpp-go): `{'texts':['parsing time \'2026-01-01 00:00:00\' as \'2006-01-02T15:04:05Z07:00\': cannot parse \' 00:00:00\' as \'T\'','[server error] error librar...`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-start-ts-offset-frac

StartTransaction timestamp "2026-01-01T09:00:00.123+09:00"

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-start-ts-offset-frac","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T09:00:00.123+09:00"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-A-start-ts-offset-frac","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}}`

### A-start-ts-no-zone

StartTransaction timestamp without zone

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","PropertyConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","PropertyConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","FormationViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema StartTransaction.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/StartTransaction.json#'\n- at '/timestamp':...`

Side (ocpp-go): `{'texts':['parsing time \'2026-01-01T00:00:00\' as \'2006-01-02T15:04:05Z07:00\': cannot parse \'\' as \'Z07:00\'','[server error] error library lo...`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-start-ts-nanos

StartTransaction timestamp with 9 fractional digits

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-start-ts-nanos","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00.123456789Z"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-A-start-ts-nanos","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}}`

### A-meter-value-number

MeterValues sampledValue.value as number

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","TypeConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","TypeConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","FormationViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema MeterValues.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/MeterValues.json#'\n- at '/meterValue/0/sampledV...`

Side (ocpp-go): `{'texts':['json: cannot unmarshal number into Go struct field .meterValue.0.sampledValue.0.value of type string','[server error] error library log ...`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-datatransfer-data-object

DataTransfer data as JSON object

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","TypeConstraintViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[3,"id1",{"data":{"a":1},"status":"Accepted"}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","TypeConstraintViolation","<desc>",{}]`
- only ocpp-go: `a: <- [3,"id1",{"data":{"a":1},"status":"Accepted"}]`

Layer `serverEvents`:

- only ocpp-go: `{"kind":"request","cpId":"P-A-datatransfer-data-object","action":"DataTransfer","payload":{"data":{"a":1},"vendorId":"v"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-A-datatransfer-data-object","action":"DataTransfer","payload":{"data":{"a":1},"status":"Accepted"}}`

Side (gocpp): `{'texts':['schema DataTransfer.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/DataTransfer.json#'\n- at '/data': got object, ...`

### B-datatransfer-panic

DataTransfer parity.panic through the simulator's data_transfer command

Layer `steps`:

- only gocpp: `result: {"error":"DataTransfer <uuid> answered with CALLERROR InternalError: internal error"}`
- only gocpp: `probe: {"reply":[3,"probe1",{"currentTime":"2026-01-01T00:00:00Z"}],"closed":null}`
- only ocpp-go: `result: {"error":"DataTransfer <uuid> dropped (socket_closed)"}`
- only ocpp-go: `probe: {"error":"A raw frame needs an open WebSocket"}`

Layer `wire`:

- only gocpp: `a: <- [4,"<uuid>","InternalError","<desc>",{}]`
- only gocpp: `a: -> [2,"probe1","Heartbeat",{}]`
- only gocpp: `a: <- [3,"probe1",{"currentTime":"2026-01-01T00:00:00Z"}]`

Layer `simEvents`:

- only ocpp-go: `a: disconnected(1006)`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-B-datatransfer-panic","action":"Heartbeat","payload":{}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-B-datatransfer-panic","action":"Heartbeat","payload":{"currentTime":"2026-01-01T00:00:00Z"}}`

Layer `serverAlive`:

- only gocpp: `serverAlive=true`
- only ocpp-go: `serverAlive=false`

Side (gocpp): `{'texts':['[server log] error handler panic recovered {\'action\':\'DataTransfer\',\'cp_id\':\'P-B-datatransfer-panic\',\'panic\':\'parity: deliber...`

Side (ocpp-go): `{'texts':['[sim a close reason] Connection ended']}`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 0

### B-datatransfer-panic-raw

DataTransfer parity.panic as a raw frame (what comes back on the wire)

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"dt1","InternalError","<desc>",{}],"closed":null}`
- only gocpp: `probe: {"reply":[3,"probe1",{"currentTime":"2026-01-01T00:00:00Z"}],"closed":null}`
- only ocpp-go: `frame: {"reply":null,"closed":"network"}`
- only ocpp-go: `probe: {"error":"A raw frame needs an open WebSocket"}`

Layer `wire`:

- only gocpp: `a: <- [4,"dt1","InternalError","<desc>",{}]`
- only gocpp: `a: -> [2,"probe1","Heartbeat",{}]`
- only gocpp: `a: <- [3,"probe1",{"currentTime":"2026-01-01T00:00:00Z"}]`

Layer `simEvents`:

- only ocpp-go: `a: disconnected(1006)`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-B-datatransfer-panic-raw","action":"Heartbeat","payload":{}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-B-datatransfer-panic-raw","action":"Heartbeat","payload":{"currentTime":"2026-01-01T00:00:00Z"}}`

Layer `serverAlive`:

- only gocpp: `serverAlive=true`
- only ocpp-go: `serverAlive=false`

Side (gocpp): `{'texts':['internal error','[server log] error handler panic recovered {\'action\':\'DataTransfer\',\'cp_id\':\'P-B-datatransfer-panic-raw\',\'pani...`

Side (ocpp-go): `{'texts':['[sim a close reason] Connection ended']}`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 0

### C-shutdown-then-reconnect

/control/shutdown, then a new connection attempt (kept under 1 s: the simulator auto-reconnects after 1 s)

Layer `steps`:

- only gocpp: `connectB: {"error":"Connection failed: code=1002 reason=Expected 101 status code"}`
- only gocpp: `bootB: false`
- only gocpp: `connections: []`
- only ocpp-go: `connectB: {"ok":true}`
- only ocpp-go: `bootB: true`
- only ocpp-go: `connections: ["P-C-shutdown-then-reconnect"]`

Layer `wire`:

- only ocpp-go: `b: -> [2,"<uuid>","BootNotification",{"chargeBoxSerialNumber":"CLI-001","chargePointModel":"CLI-Model","chargePointSerialNumber":"CLI-001","chargePointVendor":"CLI-Vendor","firmwareVersion":"1.0.0","iccid":"","imsi":"","meterSerialNumber":"CLI-M001","meterType":""}]`
- only ocpp-go: `b: <- [3,"<uuid>",{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}]`
- only ocpp-go: `b: -> [2,"<uuid>","StatusNotification",{"connectorId":0,"errorCode":"NoError","status":"Available"}]`
- only ocpp-go: `b: <- [3,"<uuid>",{}]`
- only ocpp-go: `b: -> [2,"<uuid>","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Available"}]`
- only ocpp-go: `b: <- [3,"<uuid>",{}]`

Layer `simEvents`:

- only gocpp: `b: disconnected(1002)`
- only ocpp-go: `b: connected`

Layer `serverEvents`:

- only ocpp-go: `{"kind":"connect","cpId":"P-C-shutdown-then-reconnect"}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-shutdown-then-reconnect","action":"BootNotification","payload":{"chargeBoxSerialNumber":"CLI-001","chargePointModel":"CLI-Model","chargePointSerialNumber":"CLI-001","chargePointVendor":"CLI-Vendor","firmwareVersion":"1.0.0","meterSerialNumber":"CLI-M001"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-shutdown-then-reconnect","action":"BootNotification","payload":{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{"connectorId":0,"errorCode":"NoError","status":"Available"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{}}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{"connectorId":1,"errorCode":"NoError","status":"Available"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{}}`

Side (gocpp): `{'texts':['[disconnect detail] P-C-shutdown-then-reconnect: ocpp: connection closed','[sim a close reason] closed','[sim b close reason] Expected 1...`

Side (ocpp-go): `{'texts':['[disconnect detail] P-C-shutdown-then-reconnect: StopConnection']}`

### C-idle-silence-75s

boot, then no traffic for 75 s (both servers use 60 s ping/read idle defaults)

Layer `steps`:

- only gocpp: `closedDuringIdle: false`
- only ocpp-go: `closedDuringIdle: true`

Layer `wire`:

- only ocpp-go: `a: -> [2,"<uuid>","BootNotification",{"chargeBoxSerialNumber":"CLI-001","chargePointModel":"CLI-Model","chargePointSerialNumber":"CLI-001","chargePointVendor":"CLI-Vendor","firmwareVersion":"1.0.0","iccid":"","imsi":"","meterSerialNumber":"CLI-M001","meterType":""}]`
- only ocpp-go: `a: <- [3,"<uuid>",{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}]`
- only ocpp-go: `a: -> [2,"<uuid>","StatusNotification",{"connectorId":0,"errorCode":"NoError","status":"Available"}]`
- only ocpp-go: `a: <- [3,"<uuid>",{}]`
- only ocpp-go: `a: -> [2,"<uuid>","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Available"}]`
- only ocpp-go: `a: <- [3,"<uuid>",{}]`

Layer `simEvents`:

- only ocpp-go: `a: disconnected(1006)`
- only ocpp-go: `a: connected`

Layer `serverEvents`:

- only ocpp-go: `{"kind":"disconnect","cpId":"P-C-idle-silence-75s","cause":"timeout"}`
- only ocpp-go: `{"kind":"connect","cpId":"P-C-idle-silence-75s"}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-idle-silence-75s","action":"BootNotification","payload":{"chargeBoxSerialNumber":"CLI-001","chargePointModel":"CLI-Model","chargePointSerialNumber":"CLI-001","chargePointVendor":"CLI-Vendor","firmwareVersion":"1.0.0","meterSerialNumber":"CLI-M001"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-idle-silence-75s","action":"BootNotification","payload":{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-idle-silence-75s","action":"StatusNotification","payload":{"connectorId":0,"errorCode":"NoError","status":"Available"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-idle-silence-75s","action":"StatusNotification","payload":{}}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-idle-silence-75s","action":"StatusNotification","payload":{"connectorId":1,"errorCode":"NoError","status":"Available"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-idle-silence-75s","action":"StatusNotification","payload":{}}`

Side (gocpp): `{'closedAtMs':null,'texts':[]}`

Side (ocpp-go): `{'closedAtMs':59887,'texts':['[disconnect detail] P-C-idle-silence-75s: read tcp 127.0.0.1:21001->127.0.0.1:51234: i/o timeout','[sim a close reaso...`

### D-csms-GetCompositeSchedule

CSMS-initiated GetCompositeSchedule answered by the simulator's engine

Layer `steps`:

- only gocpp: `control: {"ok":true,"result":{"chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[],"duration":60},"connectorId":1,"scheduleStart":"<now>","status":"Accepted"}}`
- only ocpp-go: `control: {"error":"<text>","kind":"timeout","ok":false}`

Layer `wire`:

- only ocpp-go: `a: <- [4,"<csms-call-1>","PropertyConstraintViolation","<desc>",{}]`

Side (gocpp): `{'texts':['[server log] warn schema soft violations passed (lenient) {\'action\':\'GetCompositeSchedule\',\'keywords\':\'[minItems]\',\'kind\':\'re...`

Side (ocpp-go): `{'texts':['[control error] Request timed out','[server error] error library log {}','Field CallResult.Payload.ChargingSchedule.ChargingSchedulePeri...`

Diagnostics (log/error events, not compared): gocpp 1, ocpp-go 2

