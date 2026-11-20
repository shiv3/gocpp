## Group E analysis (hand-written, appended by the runner from `analysis.md`)

### How a scenario is driven

`E-<scenario id>` starts the simulator, connects, waits for the BootNotification answer and starts the bundled template with `run_scenario_template` (connector 1). It then polls `scenario_status` every 100 ms. Scenario nodes that wait for a CSMS request (`remoteStartTrigger`, `reservationTrigger`, `csmsCallTrigger`, ...) put the run into `state: waiting` with an `expectation` (`action`, direction `CSMS_TO_CP`). The per-scenario script (table in `runner/scenarios.ts`, reproduced above under "CSMS side per scenario") says "when the scenario waits for action X, send these `/control/call`s", or "when the run reaches node N", or runs a simulator command. No fixed sleeps are used to synchronise with the scenario (the few `sleep`s are inside the simulator-driven scripts, see below). A case ends when the run reaches `completed`/`error`, or when its budget (55 s, shorter for the open-ended ones) runs out; then `stop_all_scenarios` and `scenario_report` are fetched. After the scenario a 1.5 s settle lets in-flight traffic (for example the reboot after Reset) finish before the sockets are torn down.

Observed per case and library: the simulator's `scenario_report` (execution state, verdict, assertions, errors, the CSMS-initiated part of the transcript, executed nodes, initial/final connector state), the control-call results, the full CP-side wire trace, the server events and liveness. The CP-initiated part of the report transcript is not compared: its last entries are cut off at the moment the run ends (a simulator timing artefact); the same frames are in the wire layer. For scenarios without assertions the simulator reports verdict `SKIPPED` (7 of the bundled scenarios carry assertions), so "pass" for those means: completed, and a per-scenario path check (`expect` in the script table, evaluated on the complete wire trace) confirmed the intended path was taken, e.g. Authorize answered Invalid/Expired/Blocked and no StartTransaction (TC_023.x), FirmwareStatusNotification sequences (TC_044.x), StopTransaction reasons (TC_005, TC_013, TC_014), the StatusNotification after TriggerMessage (TC_054), and so on. Whenever such a check failed, the case would show `EXPECTED PATH NOT TAKEN` and count as failing; none does.

Outcome classes used above: pass = completed, no FAIL verdict, path check ok; fail = FAIL verdict, error, path not taken or never reached the expectation; bounded = open-ended by design and stopped by the runner at its budget (state at that moment compared).

### Scenarios not run

- `dataflow-authorize-branch-201`: OCPP 2.0.1 only (idToken payload).
- `dataflow-remote-start-reauthorize-201`: OCPP 2.0.1 only (RequestStartTransaction).

68 bundled scenario files: 66 run, 2 skipped. 68 cases run: the 66 plus two variants of a bundled scenario: `cert16-octt-strictness-probe+csms-sends` (the CSMS does what the probe warns about) and `remote-start-timeout-fallback+timeout` (nothing is sent, the 30 s wait times out).

### Result

- Passing on both with identical outcome: 64.
- Bounded on both with identical state: 3 (`full-charging-cycle`, `status-triggered-actions`, `multi-status-monitor`), see below.
- Failing on both the same way: 1 (`resilience-communication-loss-recovery`), see below.
- Differing between gocpp and ocpp-go: 0. In no E case does any layer differ: not the scenario report, not the control-call results, not a single normalised wire frame (CSMS-initiated frames differ only in the message id format, ULID vs decimal, which is normalised), not the server events.

### Failing on both

`resilience-communication-loss-recovery`: report `completed / FAIL`, assertion `link-probe-sent` failed with `no sent CALL found for action=DataTransfer (occurrence 0)`. The probe is on the wire (verified in the CP-side trace: DataTransfer `link-probe` is sent after the reconnect and answered Accepted by both servers) but the run's transcript, which the assertion reads, stops at the reconnect's StatusNotification. This is a simulator limitation, not a runner or server problem: it was reproduced when the link was cut from the simulator (`disconnect` + `connect`, as in the case) and when the server dropped the socket (ocpp-go `/control/disconnect`, simulator `connect` afterwards). The sibling scenario `resilience-power-loss-recovery`, which uses the same cut, passes both assertions.

### Bounded on both

- `full-charging-cycle`: the meter node runs to 25 kWh at 500 Wh per 5 s, about 250 s. Stopped by the runner after 25 s; both servers see the same traffic up to that point.
- `status-triggered-actions`: the status trigger re-fires on every Available (Heartbeat loop with a 10 s delay), it never ends by design. Stopped after 25 s.
- `multi-status-monitor`: three status-trigger branches; driven with simulator commands (plug in, start transaction, set Faulted); the run stays at `end-faulted` waiting for the join of the other branch, whose meter node never ends. Stopped after 25 s.

### Notes on scripts, CSMS answers and the harness

- idTag rules: the bundled `cert16-tc023-*` and `resilience-authorization-rejected` scenarios hard-code the tags `CERT023-INV`, `CERT023-EXP`, `CERT023-BLK` and `RESIL-BLK`, which the old prefix rules (INVALID / BLOCKED / EXPIRED) answer Accepted. The CSMS harness gap was closed in `common/canned.go`, which both binaries share, so both get the identical rule: idTag suffix `-INV` -> Invalid, `-BLK` -> Blocked, `-EXP` -> Expired (README updated). All other tags used by the scenarios (`CERT0xx`, `TAG001`, `DEMO-TAG-1`, ...) are Accepted by the default rule, which is what those scenarios expect.
- No other canned answer had to change. DataTransfer vendor `com.example.cert16` (TC_064) and `org.example.dataflow` are answered Accepted by the echo rule (the scenarios accept any answer; `dataflow-call-error-handling` takes its Accepted branch).
- Statuses that the scenario itself decides (ReserveNow Faulted/Occupied/Unavailable/Rejected, RemoteStart/RemoteStop Rejected, SendLocalList Failed, TriggerMessage Rejected, UnlockConnector Unlocked/UnlockFailed, TC_043.1/TC_042.1 with the list disabled) are produced by the simulator's response overrides and arming nodes, not by the CSMS.
- GetDiagnostics (TC_045.1) uploads with an HTTP POST to the `location` the CSMS sends; the runner serves a local HTTP sink on 127.0.0.1 for that (one hit, `POST /diag`). UpdateFirmware (TC_044.x) is purely simulated by the simulator: it never fetches the location (no hit).
- `essential-cp-behavior`: its meter node uses `stopMode: evSettings`; with the bundled 75 kWh EV (20 -> 80 %) it never ends, so the RemoteStop trigger after it is unreachable. The case overrides the EV (1 kWh, 78 -> 80 %) through the `evSettings` parameter of `run_scenario_template`.
- `smart-charging`: the Finishing status caused by stopping the transaction is emitted before the `trigger-finish` node starts waiting, so the trigger would never fire. The script sets Finishing once more with `update_connector_status` when the node is reached. Charging/Finishing are produced with simulator commands (plug in, start/stop transaction); no CSMS call is involved.
- `resilience-communication-loss-recovery` / `resilience-power-loss-recovery`: the link is cut and restored with the simulator's own `disconnect` / `connect` (the server cannot drop a connection in gocpp: `/control/disconnect` is unsupported there).
- `dataflow-remote-start-reauthorize-16`: passes, but note a simulator quirk: the scenario Authorizes the tag read from the CALL (`DEMO-TAG-2`), then its `transaction start` node, which has no `tagId`, starts the transaction with the default tag `123456` instead of the authorized one.
- `cert16-tc011-remote-start-stop`: the StopTransaction after RemoteStop is sent by the simulator's own handling of RemoteStopTransaction; it is on the wire but not in the run transcript (same cut-off as above), so the path check for it reads the wire trace.
- Scenario ids: `run_scenario_template` returns the loaded scenario id with a `-c1` suffix; status and report calls need that id.
- Reset (TC_013/014): both servers see the reboot (new BootNotification, StatusNotification) right after the Reset answer; Hard additionally closes the socket (close code 1000, `peer_closed` on both). With the 1.5 s settle this is identical on both libraries (without the settle the end of the case raced with the reboot traffic and showed up as a spurious serverEvents difference).
- Runner normalisation added for this group: the local ports in `ws://127.0.0.1:PORT` (OcppServer configuration key) and `http://127.0.0.1:PORT` (firmware/diagnostics location) are replaced by `127.0.0.1:<port>` (server ports differ between the two libraries' processes by construction).

### A-D regression check

The A-D rows of this run are the same set as in `2026-11-20/report.md`: 68 same / 20 differ (88 cases; `D-csms-SetChargingProfile` is now same, and the slow `C-idle-silence-75s` is not part of this run, see below). The 20 differing A-D cases are the same ones as before; no A-D case changed verdict except the intended `D-csms-SetChargingProfile`.

### Full run including the slow case

A separate single run of all cases without `--skip-slow` (`--runs 1`, 157 cases) gave 136 same / 21 differ, 0 flaky. The extra difference is `C-idle-silence-75s` (unchanged from `2026-11-20/report.md`: ocpp-go closes the idle connection with cause `timeout`, gocpp does not within the observation window). Group E in that run: 68 same, 0 differ.
