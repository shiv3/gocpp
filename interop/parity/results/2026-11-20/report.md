# Parity report 2026-11-20

Same simulator traffic against `cs-gocpp` (this checkout, cacddf6) and `cs-ocppgo` (github.com/shiv3/ocpp-go@v0.17.2-fix.3).
Simulator: `/work/wt/ocpp-cp-sim-parity` (JSON Lines mode, `--reconnect-max-attempts 0`). Runs: 2.

Re-run: `bun run interop/parity/runner/main.ts [--only <glob>] [--runs N] [--skip-slow]` (needs `make` in `interop/parity` first).

Totals: 89 cases run, 67 same, 22 differ, 0 flaky, 1 skipped.

Compared layers: `steps` (what the simulator observed for each command or frame), `wire` (every frame in the simulator's trace, from the CP side, ids/timestamps/descriptions normalised), `simEvents` (connected/disconnected with close code), `serverEvents` (connect/disconnect cause/request/response_sent/panic), `serverAlive`. Library log/error events are not compared (they are library specific); free-text such as CallError descriptions is kept in `raw/run1/<case>/<lib>/side.json`.

## Skipped

- A-callerror-answering-csms-request: the simulator answers CSMS requests from its own engine immediately; send_raw_frame cannot race it, and there is no hook to suppress the automatic answer

## Flaky

none

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
| A-boot-missing-vendor | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','OccurrenceConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-status-connid-string | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-status-enum-case | boot=true; frame={'reply':[3,'id1',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','GenericError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-status-enum-unknown | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','GenericError','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-authorize-idtag-21 | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'}}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','PropertyConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
| A-authorize-missing-idtag | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','OccurrenceConstraintViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire) |
| A-start-ts-space | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-start-ts-offset-frac | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (serverEvents) |
| A-start-ts-no-zone | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-start-ts-nanos | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (serverEvents) |
| A-start-ts-ok | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'idTagInfo':{'status':'Accepted'},'transactionId':1}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-meter-value-number | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | same |
| A-datatransfer-data-object | boot=true; frame={'reply':[4,'id1','FormationViolation','<desc>',{}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; frame={'reply':[3,'id1',{'data':{'a':1},'status':'Accepted'}],'closed':null}; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | DIFF (steps, wire, serverEvents) |
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
| C-server-disconnect | boot=true; control={'error':'unsupported','kind':'other','ok':false}; connections=['P-C-server-disconnect']; probe={'reply':[3,'probe1',{'currentTime':'2026-01-01T00:00:00Z'}],'closed':null} | boot=true; control={'ok':true}; connections=[]; probe={'error':'A raw frame needs an open WebSocket'}; sim a: disconnected(1000); srv disc P-C-server-disconnect:server_closed | DIFF (steps, wire, simEvents, serverEvents) |
| C-shutdown-then-reconnect | bootA=true; control={'ok':true}; alive=true; connectB={'ok':true}; bootB=false; connections=[]; sim a: disconnected(1000); sim b: disconnected(1000); srv disc P-C-shutdown-then-reconnect:server_closed,P-C-shutdown-then-reconnect:read_error | bootA=true; control={'ok':true}; alive=true; connectB={'ok':true}; bootB=true; connections=['P-C-shutdown-then-reconnect']; sim a: disconnected(1000); srv disc P-C-shutdown-then-reconnect:server_closed | DIFF (steps, wire, simEvents, serverEvents) |
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
| D-csms-SetChargingProfile | boot=true; control={'error':'<text>','kind':'invalid_request','ok':false} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | DIFF (steps, wire) |
| D-csms-ClearChargingProfile | boot=true; control={'ok':true,'result':{'status':'Unknown'}} | boot=true; control={'ok':true,'result':{'status':'Unknown'}} | same |
| D-csms-GetCompositeSchedule | boot=true; control={'ok':true,'result':{'chargingSchedule':{'chargingRateUnit':'W','chargingSchedulePeriod':[],'duration':60},'connectorId':1,'scheduleStart':'<now>',... | boot=true; control={'error':'<text>','kind':'timeout','ok':false} | DIFF (steps, wire) |
| D-csms-GetLocalListVersion | boot=true; control={'ok':true,'result':{'listVersion':0}} | boot=true; control={'ok':true,'result':{'listVersion':0}} | same |
| D-csms-SendLocalList | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | boot=true; control={'ok':true,'result':{'status':'Accepted'}} | same |
| D-csms-UpdateFirmware | boot=true; control={'ok':true,'result':{}} | boot=true; control={'ok':true,'result':{}} | same |
| D-csms-GetDiagnostics | boot=true; control={'ok':true,'result':{'fileName':'diagnostics.txt'}} | boot=true; control={'ok':true,'result':{'fileName':'diagnostics.txt'}} | same |

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

- only gocpp: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","OccurrenceConstraintViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","FormationViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","OccurrenceConstraintViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema BootNotification.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/BootNotification.json#'\n- at '': missing p...`

Side (ocpp-go): `{'texts':['Field Call.Payload.ChargePointVendor required but not found for feature BootNotification','[server error] error library log {}']}`

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

- only gocpp: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","GenericError","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","FormationViolation","<desc>",{}]`
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

- only gocpp: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[4,"id1","OccurrenceConstraintViolation","<desc>",{}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","FormationViolation","<desc>",{}]`
- only ocpp-go: `a: <- [4,"id1","OccurrenceConstraintViolation","<desc>",{}]`

Side (gocpp): `{'texts':['schema Authorize.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/Authorize.json#'\n- at '': missing property 'idTag...`

Side (ocpp-go): `{'texts':['Field Call.Payload.IdTag required but not found for feature Authorize','[server error] error library log {}']}`

Diagnostics (log/error events, not compared): gocpp 0, ocpp-go 2

### A-start-ts-offset-frac

StartTransaction timestamp "2026-01-01T09:00:00.123+09:00"

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-start-ts-offset-frac","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T09:00:00.123+09:00"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-A-start-ts-offset-frac","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}}`

### A-start-ts-nanos

StartTransaction timestamp with 9 fractional digits

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-A-start-ts-nanos","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00.123456789Z"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-A-start-ts-nanos","action":"StartTransaction","payload":{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01T00:00:00Z"}}`

### A-datatransfer-data-object

DataTransfer data as JSON object

Layer `steps`:

- only gocpp: `frame: {"reply":[4,"id1","FormationViolation","<desc>",{}],"closed":null}`
- only ocpp-go: `frame: {"reply":[3,"id1",{"data":{"a":1},"status":"Accepted"}],"closed":null}`

Layer `wire`:

- only gocpp: `a: <- [4,"id1","FormationViolation","<desc>",{}]`
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

### C-server-disconnect

server-initiated /control/disconnect

Layer `steps`:

- only gocpp: `control: {"error":"unsupported","kind":"other","ok":false}`
- only gocpp: `connections: ["P-C-server-disconnect"]`
- only gocpp: `probe: {"reply":[3,"probe1",{"currentTime":"2026-01-01T00:00:00Z"}],"closed":null}`
- only ocpp-go: `control: {"ok":true}`
- only ocpp-go: `connections: []`
- only ocpp-go: `probe: {"error":"A raw frame needs an open WebSocket"}`

Layer `wire`:

- only gocpp: `a: -> [2,"probe1","Heartbeat",{}]`
- only gocpp: `a: <- [3,"probe1",{"currentTime":"2026-01-01T00:00:00Z"}]`

Layer `simEvents`:

- only ocpp-go: `a: disconnected(1000)`

Layer `serverEvents`:

- only gocpp: `{"kind":"request","cpId":"P-C-server-disconnect","action":"Heartbeat","payload":{}}`
- only gocpp: `{"kind":"response_sent","cpId":"P-C-server-disconnect","action":"Heartbeat","payload":{"currentTime":"2026-01-01T00:00:00Z"}}`
- only ocpp-go: `{"kind":"disconnect","cpId":"P-C-server-disconnect","cause":"server_closed"}`

Side (ocpp-go): `{'texts':['[disconnect detail] P-C-server-disconnect: StopConnection']}`

### C-shutdown-then-reconnect

/control/shutdown, then a new connection attempt (kept under 1 s: the simulator auto-reconnects after 1 s)

Layer `steps`:

- only gocpp: `bootB: false`
- only gocpp: `connections: []`
- only ocpp-go: `bootB: true`
- only ocpp-go: `connections: ["P-C-shutdown-then-reconnect"]`

Layer `wire`:

- only ocpp-go: `b: <- [3,"<uuid>",{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}]`
- only ocpp-go: `b: -> [2,"<uuid>","StatusNotification",{"connectorId":0,"errorCode":"NoError","status":"Available"}]`
- only ocpp-go: `b: <- [3,"<uuid>",{}]`
- only ocpp-go: `b: -> [2,"<uuid>","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Available"}]`
- only ocpp-go: `b: <- [3,"<uuid>",{}]`

Layer `simEvents`:

- only gocpp: `b: disconnected(1000)`

Layer `serverEvents`:

- only gocpp: `{"kind":"disconnect","cpId":"P-C-shutdown-then-reconnect","cause":"read_error"}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-shutdown-then-reconnect","action":"BootNotification","payload":{"chargeBoxSerialNumber":"CLI-001","chargePointModel":"CLI-Model","chargePointSerialNumber":"CLI-001","chargePointVendor":"CLI-Vendor","firmwareVersion":"1.0.0","meterSerialNumber":"CLI-M001"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-shutdown-then-reconnect","action":"BootNotification","payload":{"currentTime":"2026-01-01T00:00:00Z","interval":300,"status":"Accepted"}}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{"connectorId":0,"errorCode":"NoError","status":"Available"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{}}`
- only ocpp-go: `{"kind":"request","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{"connectorId":1,"errorCode":"NoError","status":"Available"}}`
- only ocpp-go: `{"kind":"response_sent","cpId":"P-C-shutdown-then-reconnect","action":"StatusNotification","payload":{}}`

Side (gocpp): `{'texts':['[disconnect detail] P-C-shutdown-then-reconnect: ocpp: connection closed','[disconnect detail] P-C-shutdown-then-reconnect: context canc...`

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

Side (ocpp-go): `{'closedAtMs':59915,'texts':['[disconnect detail] P-C-idle-silence-75s: read tcp 127.0.0.1:21001->127.0.0.1:57280: i/o timeout','[sim a close reaso...`

### D-csms-SetChargingProfile

CSMS-initiated SetChargingProfile answered by the simulator's engine

Layer `steps`:

- only gocpp: `control: {"error":"<text>","kind":"invalid_request","ok":false}`
- only ocpp-go: `control: {"ok":true,"result":{"status":"Accepted"}}`

Layer `wire`:

- only ocpp-go: `a: <- [2,"<csms-call-1>","SetChargingProfile",{"connectorId":1,"csChargingProfiles":{"chargingProfileId":1,"chargingProfileKind":"Absolute","chargingProfilePurpose":"TxDefaultProfile","chargingSchedule":{"chargingRateUnit":"W","chargingSchedulePeriod":[{"limit":1000,"startPeriod":0}]},"stackLevel":0}}]`
- only ocpp-go: `a: -> [3,"<csms-call-1>",{"status":"Accepted"}]`

Side (gocpp): `{'texts':['[control error] schema SetChargingProfile.json: jsonschema validation failed with 'file:///work/wt/gocpp-audit/SetChargingProfile.json#'...`

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

