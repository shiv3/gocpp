# Staged migration from ocpp-go: `WithOCPPGoCompat`

> **No code changes needed?** If you want gocpp's runtime but keep your ocpp-go code,
> use the drop-in modules instead: one `replace` line in `go.mod`
> (`github.com/shiv3/gocpp/compat/ocppgo v0.17.2-gocpp.1` for ocpp-go v0.17.x,
> `github.com/shiv3/gocpp/compat/ocppgo-v019 v0.19.0-gocpp.1` for v0.19.0).
> See [the README](../README.md#drop-in-replacement-for-ocpp-go) and
> [compat/ocppgo/ROLLOUT.md](../compat/ocppgo/ROLLOUT.md). This page is for moving to
> the native gocpp API.

[migration-from-lorenzodonini.md](migration-from-lorenzodonini.md) maps the API.
This page is about what **chargers see**. A differential test sent the same
charger traffic (OCPP 1.6) to a gocpp CSMS and to an ocpp-go CSMS; the replies
differ in a handful of places. Where ocpp-go follows the spec, gocpp does too, and
the rows below are only differences. Where chargers in the field were tuned
against ocpp-go's behaviour, `csms.WithOCPPGoCompat()` reproduces it, so the
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
| `WithLenientSchema()` | on | rows 1-5 |
| `WithErrorCodeMapper(dispatcher.OCPPGoErrorCodeMapper())` | on | rows 6-11 |
| `WithMessageIDPolicy(dispatcher.MessageIDDrop)` | drop | row 12 |
| `WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds})` | UTC, whole seconds | row 13 |
| `WithWebSocketPingInterval(0)` | no server pings | row 14 |
| `WithWebSocketReadTimeout(60 * time.Second)` | 60 s idle close | row 14 |

## Differences

"Default" is gocpp without the preset; "ocpp-go" is `v0.17.x`. The spec column says
which clause gocpp's default follows. Strict schema mode is unchanged by the preset
row by row except where stated.

| # | Situation | gocpp default | gocpp compat | ocpp-go | Spec |
|---|---|---|---|---|---|
| 1 | `[2,"id","Heartbeat",null]` (payload `null`) | lenient: accepted, `null` read as `{}` (soft `type`); strict/tolerant: FormationViolation | accepted | accepted | OCPP-J 1.6 §4.2: payload is a JSON object |
| 2 | 1.6 `DataTransfer.data` is an object, array, number or boolean | lenient: coerced to its compact JSON text (soft `type`), handler sees a string; strict: TypeConstraintViolation | coerced | accepted (`interface{}`) | 1.6 schema: `data` is a string |
| 3 | Zone-less or space-separated timestamps, numbers as strings, other near-miss primitives | lenient repairs them (soft `format` / `type`); strict rejects | repaired | rejected (FormationViolation) | RFC 3339 / schema types |
| 4 | Unknown enum value | PropertyConstraintViolation | GenericError (see 7) | GenericError | 1.6 §4.2.3 |
| 5 | Enum value differing only in case, `maxLength`/range overshoot, extra properties | lenient: accepted (soft); strict: rejected | accepted | rejected | schema |
| 6 | Unknown action | `NotImplemented` | `NotSupported` | `NotSupported` | NotImplemented = action not known; NotSupported = known but not supported |
| 7 | Missing required property | `OccurenceConstraintViolation` on 1.6 (the spec's spelling), `OccurrenceConstraintViolation` on 2.x | `OccurrenceConstraintViolation` also on 1.6 | `OccurrenceConstraintViolation` on both | 1.6 §4.2.3 spells it with one `r` |
| 8 | Length or range violation | PropertyConstraintViolation | PropertyConstraintViolation | PropertyConstraintViolation | |
| 9 | Wrong JSON type of a property, bad date-time, payload not an object | TypeConstraintViolation / PropertyConstraintViolation / FormationViolation by keyword | FormationViolation (FormatViolation on 2.x) | FormationViolation | 1.6 §4.2.3 |
| 10 | Unknown MessageTypeId on a 1.6 connection | no reply (1.6 has no such code); 2.x: `MessageTypeNotSupported` | `MessageTypeNotSupported` | `MessageTypeNotSupported` | 1.6 error table lacks the code, 2.x Part 4 §4.2.3 has it |
| 11 | CALL with fewer than 3 elements, or a non-string action | FormationViolation (id echoed) | no reply | no reply | the id is readable, so the spec asks for an answer |
| 12 | Message id empty or longer than 36 characters | strict: over-long id answered with FormationViolation (id echoed), empty id dropped; lenient/tolerant: accepted and logged | dropped silently | dropped silently | OCPP-J: `messageId` is at most 36 characters |
| 13 | Outbound date-time | as `encoding/json` writes `time.Time`: RFC 3339, original offset, fractional seconds when present | UTC (`Z`), whole seconds | UTC, whole seconds (`time.RFC3339`) | RFC 3339 |
| 14 | Idle connection | server pings every 54 s (the charger's pong keeps the link alive); closes after 60 s with no frame, ping or pong | no server pings; closes after 60 s without any inbound frame, so the charger must send traffic or pings | no server pings; read deadline 60 s, reset by any frame or ping | RFC 6455 §5.5 allows either side to ping |

Replies that stay different on purpose, with or without the preset:

- A panic in a handler is recovered and answered with `InternalError`; ocpp-go
  crashes the process.
- After `Shutdown` / `Close`, new upgrades get HTTP 503; ocpp-go keeps accepting.
- Lenient mode accepts what ocpp-go rejects in rows 3 and 5. It is the safer
  direction for a migration: no charger that worked on ocpp-go can be refused.

## The individual options

| Option | What it does |
|---|---|
| `WithLenientSchema()` | Existing. Now also reads a `null` payload as `{}` and coerces structured `data` strings. |
| `WithErrorCodeMapper(func(dispatcher.ErrorContext) (wireCode string, reply bool))` | Changes the code, or silences the reply, of errors gocpp generates itself (unknown action, schema rejection, undecodable payload, malformed frame, unsupported message type, bad message id). Never touches an error a handler returns. The returned code is sent exactly as given. `dispatcher.OCPPGoErrorCodeMapper()` implements rows 6-11. |
| `WithMessageIDPolicy(p)` | `MessageIDAuto` (default: reject when strict, accept and log otherwise), `MessageIDAccept`, `MessageIDReject`, `MessageIDDrop`. |
| `WithTimeEncoding(schema.TimeEncoding{UTC, Precision})` | Rewrites every `"format": "date-time"` field of every outbound CALL, CALLRESULT and SEND. `Precision` is `TimePrecisionAuto` (default), `Seconds`, `Millis`, `Micros` or `Nanos`; fixed width, fractions truncated, never rounded. Works through the schema registry (`WithSchemaRegistry`), on 1.6, 2.0.1 and 2.1, with no global state. |
| `WithWebSocketPingInterval` / `WithWebSocketPongWait` / `WithWebSocketReadTimeout` | Existing keepalive knobs; `0` disables pings or the idle check. |

## Verification

The differential harness (`interop/parity` on the `test/ocppgo-parity` branch)
replays 67 raw-frame, fault and connection cases (groups A, B and C) against both
libraries. gocpp without the preset differed from ocpp-go in 24 of them; with
`WithOCPPGoCompat()` 13 remain, all deliberate:

- `A-status-connid-string`, `A-status-enum-case`, `A-authorize-idtag-21`,
  `A-start-ts-space`, `A-start-ts-no-zone`, `A-meter-value-number`: lenient mode
  accepts (and repairs) what ocpp-go rejects.
- `A-start-ts-offset-frac`, `A-start-ts-nanos`: the wire is identical; only the
  timestamp the handler records differs (gocpp keeps the offset and fraction).
- `A-datatransfer-data-object`: both accept; a handler that echoes `data` back
  sends it as a JSON string under gocpp (the 1.6 type) and as an object under
  ocpp-go.
- `B-datatransfer-panic`, `B-datatransfer-panic-raw`: gocpp recovers the panic.
- `C-shutdown-then-reconnect`: gocpp refuses upgrades while shutting down.
- `C-idle-silence-75s`: both close at 60 s; gocpp closes with a normal close frame
  (1000), ocpp-go drops the TCP connection (1006).
