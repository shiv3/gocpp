# Staged migration from ocpp-go: `WithOCPPGoCompat`

> **No code changes needed?** If you want gocpp's runtime but keep your ocpp-go code,
> use the drop-in module instead: one `replace` line in `go.mod`
> (`github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.2` for ocpp-go v0.19.0).
> See [the README](../README.md#drop-in-replacement-for-ocpp-go) and
> [compat/ocppgo/README.md](../compat/ocppgo/README.md). This page is for
> moving to the native gocpp API.

[migration-from-lorenzodonini.md](migration-from-lorenzodonini.md) maps the API.
This page is about what **chargers see**. A differential test sent the same
charger traffic (OCPP 1.6) to a gocpp CSMS and to a CSMS on upstream ocpp-go
v0.19.0; the replies differ in a handful of places. Where ocpp-go follows the spec, gocpp does too, and
the rows below are only differences. Where chargers in the field were tuned
against ocpp-go's behaviour, `csms.WithOCPPGoCompat()` reproduces it (the
behaviour of **upstream v0.19.0**), so the
migration can be done in two steps: swap the library with the preset on, then
turn the individual behaviours off one at a time.

```go
reg := schema.NewRegistry()
_ = v16.RegisterSchemas(reg)
srv := csms.NewServer(
    csms.WithSubProtocols("ocpp1.6"),
    csms.WithSchemaRegistry(reg), // the preset's lenient mode and time encoding need it
    csms.WithOCPPGoCompat(),
    // later options win: put one behaviour back to the gocpp default
    csms.WithWebSocketPingInterval(54*time.Second),
)
```

`cp.WithOCPPGoCompat()` is the same preset for the charge-point client (simulators
and test rigs written against ocpp-go), minus the keepalive part, which already
matches.

## What the preset sets

`WithOCPPGoCompat()` is only shorthand for these options, applied in this order.
Each one works on its own.

| Option | Preset value | Needed for |
|---|---|---|
| `WithOCPPGoValidation()` | ocpp-go's validation policy (strict, plus what ocpp-go accepts) | rows 1-5, 3b |
| `WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper())` | on | rows 6-11b |
| `WithMessageIDPolicy(dispatcher.MessageIDDrop)` | drop | row 12 |
| `WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds})` | UTC, whole seconds | row 13 |
| `WithWebSocketPingInterval(0)` | no server pings | row 14 |
| `WithWebSocketReadTimeout(60 * time.Second)` | 60 s idle close | row 14 |
| `WithAbruptIdleClose()` | idle connection dropped without a close frame | row 14 |
| `WithDuplicatePolicy(DuplicatePolicyRejectNewAfterUpgrade)` | first connection wins | row 15 |
| `WithKeepAcceptingAfterShutdown()` | `Shutdown` closes the current connections, upgrades stay accepted | row 18 |
| `WithUnlimitedMessageSize()` | no inbound size limit | row 16 |

## Differences

"Default" is gocpp without the preset; "ocpp-go" is upstream `v0.19.0` (every row was checked against its source and by the differential harness). The spec column says
which clause gocpp's default follows. Strict schema mode is unchanged by the preset
row by row except where stated.

| # | Situation | gocpp default | gocpp compat | ocpp-go | Spec |
|---|---|---|---|---|---|
| 1 | `[2,"id","Heartbeat",null]` (payload `null`) | lenient: accepted, `null` read as `{}` (soft `type`); strict/tolerant: FormationViolation | accepted | accepted | OCPP-J 1.6 §4.2: payload is a JSON object |
| 2 | 1.6 `DataTransfer.data` is an object, array, number or boolean | lenient: coerced to its compact JSON text (soft `type`), handler sees a string; strict: TypeConstraintViolation | accepted as is: structured JSON in `data` is coerced to its JSON text (handler sees a string) | accepted (`interface{}`) | 1.6 schema: `data` is a string |
| 3 | Zone-less timestamp (`2026-01-01T00:00:00`) | lenient: accepted; strict: rejected | accepted | accepted | RFC 3339 requires an offset |
| 3b | Space-separated timestamps, numbers as strings, other near-miss primitives | lenient repairs them (soft `format` / `type`); strict rejects | rejected (FormationViolation) | rejected (FormationViolation) | RFC 3339 / schema types |
| 4 | Unknown enum value | PropertyConstraintViolation | GenericError (see 7) | GenericError | 1.6 §4.2.3 |
| 5 | Enum value differing only in case, `maxLength`/range overshoot, extra properties | lenient: accepted (soft); strict: rejected | rejected (enum case: GenericError, maxLength / range: PropertyConstraintViolation); unknown properties accepted | rejected | schema |
| 6 | Unknown action | `NotImplemented` | `NotSupported` | `NotSupported` | NotImplemented = action not known; NotSupported = known but not supported |
| 7 | Missing required property | `OccurenceConstraintViolation` on 1.6 (the spec's spelling), `OccurrenceConstraintViolation` on 2.x | `OccurrenceConstraintViolation` also on 1.6 | `OccurrenceConstraintViolation` on both | 1.6 §4.2.3 spells it with one `r` |
| 8 | Length or range violation | PropertyConstraintViolation | PropertyConstraintViolation | PropertyConstraintViolation | |
| 9 | Wrong JSON type of a property, bad date-time, payload not an object | TypeConstraintViolation / PropertyConstraintViolation / FormationViolation by keyword | FormationViolation (FormatViolation on 2.x) | FormationViolation | 1.6 §4.2.3 |
| 10 | Unknown MessageTypeId on a 1.6 connection | no reply (1.6 has no such code); 2.x: `MessageTypeNotSupported` | `MessageTypeNotSupported` | `MessageTypeNotSupported` | 1.6 error table lacks the code, 2.x Part 4 §4.2.3 has it |
| 11 | Array with fewer than 3 elements | FormationViolation (id echoed when readable) | no reply | no reply (no id to answer with) | the id is readable, so the spec asks for an answer |
| 11b | CALL with 3 or 5+ elements, or a non-string action | FormationViolation (id echoed) | FormationViolation (id echoed) | FormationViolation (id echoed) | OCPP-J 1.6 §4.2.3 |
| 12 | Message id empty or longer than 36 characters | strict: over-long id answered with FormationViolation (id echoed), empty id dropped; lenient/tolerant: accepted and logged | dropped silently | dropped silently | OCPP-J: `messageId` is at most 36 characters |
| 13 | Outbound date-time | as `encoding/json` writes `time.Time`: RFC 3339, original offset, fractional seconds when present | UTC (`Z`), whole seconds | UTC, whole seconds (`time.RFC3339`) | RFC 3339 |
| 14 | Idle connection | server pings every 54 s (the charger's pong keeps the link alive); closes after 60 s with no frame, ping or pong, with a close frame (1000) | no server pings; closes after 60 s without any inbound frame, dropping the TCP connection without a close frame (charger sees 1006); `WithGracefulIdleClose()` sends 1000 | no server pings; read deadline 60 s, reset by any frame or ping; the connection is dropped (1006) | RFC 6455 §5.5 allows either side to ping |
| 15 | Second connection with the id of a connected charger | last wins: the old socket is closed (1008), the new one is served | first wins: the new socket is upgraded, then closed with 1008 "a connection with this ID already exists"; `WithDuplicatePolicy(DuplicatePolicyCloseExisting)` after the preset puts last wins back | first wins: the new socket is upgraded, then closed with 1008 "a connection with this ID already exists" | OCPP-J 1.6 §3.1 is silent; a charger that reconnects after a half-open drop is locked out under first-wins |
| 16 | Inbound frame larger than 1 MiB | connection closed with 1009 | no limit; `WithMaxMessageSize(n)` after the preset sets one | no limit | unspecified; an unlimited read lets any peer exhaust memory |
| 17 | Application handler panics | recovered, `InternalError`, connection and process survive | same | process crashes | |
| 18 | Upgrade after `Shutdown` | HTTP 503 | upgrades stay accepted: `Shutdown` closes the connections that exist and nothing more (`WithRefuseUpgradesAfterShutdown()` puts the 503 back) | accepted (ocpp-go closes its listener when the embedder stops the HTTP server) | |

## Risky parts of the preset, and how to put gocpp's behaviour back

The preset reproduces ocpp-go's charger-visible behaviour, including three
choices that are less safe than gocpp's defaults (rows 14 to 16). Each can be
reverted by an option listed after `WithOCPPGoCompat()`:

| Preset behaviour | Risk | Put gocpp's behaviour back |
|---|---|---|
| first connection wins (`DuplicatePolicyRejectNewAfterUpgrade`) | a charger that lost its connection without a FIN is refused until the server notices (up to the read timeout) and, with ocpp-go's own client, keeps retrying | `WithDuplicatePolicy(csms.DuplicatePolicyCloseExisting)`; `DuplicatePolicyRejectNew` is first-wins with an HTTP 409 instead of an upgrade |
| no inbound size limit (`WithUnlimitedMessageSize()`, `cp.` too) | an unauthenticated peer can make the process buffer any amount of memory | `WithMaxMessageSize(n)` (1 MiB is gocpp's default) |
| idle connection dropped without a close frame | the charger cannot tell an idle close from a network failure | `WithGracefulIdleClose()` |
| strict validation (ocpp-go's) | chargers that send near-miss values (wrong case, string numbers, space-separated timestamps) are rejected, as on ocpp-go | `WithLenientSchema()` |
| upgrades accepted after `Shutdown` | connections that arrive afterwards are never closed by `Shutdown`; call `Close` last | `WithRefuseUpgradesAfterShutdown()` |

Not reproduced, on purpose: a panicking handler kills the ocpp-go process;
gocpp contains it (row 17).

Replies that stay different on purpose, with or without the preset:

- A panic in a handler is recovered and answered with `InternalError`; ocpp-go
  crashes the process.
- After `Shutdown` / `Close`, new upgrades get HTTP 503; ocpp-go keeps accepting.
- Lenient mode accepts what ocpp-go rejects in rows 3 and 5. It is the safer
  direction for a migration: no charger that worked on ocpp-go can be refused.

## The individual options

| Option | What it does |
|---|---|
| `WithOCPPGoValidation()` | Server and client. Lenient mode with ocpp-go's policy: hard rejection of every schema violation except unknown properties, a null payload, a zone-less date-time and structured `data`; no repairs, no enum case folding. In the preset. `WithLenientSchema()` after it selects the broader repairs. |
| `WithAnswerInvalidResponses(bool)` | Server and client. A CALLRESULT that fails validation is answered with a CALLERROR carrying the request's id (OCPP 1.6 / 2.0.1), as ocpp-go does. In the preset. |
| `WithKeepAcceptingAfterShutdown()` / `WithRefuseUpgradesAfterShutdown()` | Server. `Shutdown` closes only the connections that exist (preset) or refuses all upgrades with 503 (default). |
| `WithLenientSchema()` | Existing. Now also reads a `null` payload as `{}` and coerces structured `data` strings. |
| `WithErrorCodeMapper(func(dispatcher.ErrorContext) (wireCode string, reply bool))` | Changes the code, or silences the reply, of errors gocpp generates itself (unknown action, schema rejection, undecodable payload, malformed frame, unsupported message type, bad message id). Never touches an error a handler returns. The returned code is sent exactly as given. `dispatcher.OCPPGoErrorCodeMapper()` implements rows 6-11b. |
| `WithMessageIDPolicy(p)` | `MessageIDAuto` (default: reject when strict, accept and log otherwise), `MessageIDAccept`, `MessageIDReject`, `MessageIDDrop`. |
| `WithTimeEncoding(schema.TimeEncoding{UTC, Precision})` | Rewrites every `"format": "date-time"` field of every outbound CALL, CALLRESULT and SEND. `Precision` is `TimePrecisionAuto` (default), `Seconds`, `Millis`, `Micros` or `Nanos`; fixed width, fractions truncated, never rounded. Works through the schema registry (`WithSchemaRegistry`), on 1.6, 2.0.1 and 2.1, with no global state. |
| `WithDuplicatePolicy(DuplicatePolicyRejectNewAfterUpgrade)` | First connection wins, the newcomer is closed with 1008 after the upgrade (ocpp-go v0.19.0). In the server preset. |
| `WithUnlimitedMessageSize()` | Server and client. No inbound size limit (ocpp-go v0.19.0). In both presets. |
| `WithAbruptIdleClose()` / `WithGracefulIdleClose()` | Server. How the read idle timeout ends a connection: TCP dropped (ocpp-go, close code 1006, in the preset) or close frame 1000 (default). |
| `WithWebSocketPingInterval` / `WithWebSocketPongWait` / `WithWebSocketReadTimeout` | Existing keepalive knobs; `0` disables pings or the idle check. |

## Verification

The differential harness (`interop/parity` on the `test/loadtest-4k` branch)
replays raw-frame, fault and connection cases (groups A to C), data-flow cases
(D) and the 68 simulator scenarios (E) against a CSMS on upstream ocpp-go v0.19.0
and one on gocpp, with and without the preset, and against the drop-in module
`compat/ocppgo`. The reports are under `interop/parity/results/` on that branch.
What remains different with the preset:

- ocpp-go validates with struct tags, gocpp with the JSON schemas, so a field with
  a schema constraint but no tag (or the reverse) is judged differently; the
  harness cases agree;
- structured DataTransfer `data` reaches the handler (and its echo) as a JSON
  string, ocpp-go keeps the object; a number in `data` is rejected;
- the timestamp a handler records keeps the offset and fraction the charger sent
  (ocpp-go's example server re-encodes it as UTC seconds);
- an invalid CALLRESULT from a charger: both send the charger a CALLERROR with the
  request's id (`WithAnswerInvalidResponses`); ocpp-go's application then hears a
  timeout, gocpp's caller the validation error at once (not charger-visible);
- the panic row (17): a handler panic is contained.

Idle close, duplicate id and frame size (rows 14 to 16) are the same as ocpp-go
in the preset.
