# 0004. Schema validation is a four-way mode: off by default, lenient for production

Status: Accepted
Date: 2026-11-25

## Context

Real chargers send payloads that a strict reading of the schema rejects: quoted
numbers, timestamps without a zone, differently cased enum values, vendor
measurands. Rejecting all of them makes a fleet unusable; accepting everything
unchecked lets structurally broken messages reach handlers. Validation also has a
cost ([performance.md](../performance.md#schema-validation-cost)).

## Decision

The mode is chosen per server or client:

| Option | Behaviour |
|---|---|
| `WithStrictSchema(false)` (the default) | no validation, even with a registry |
| `WithTolerantSchema()` | validate, log violations, process anyway |
| `WithLenientSchema()` | reject structurally broken messages; repair or log the benign ones |
| `WithStrictSchema(true)` | reject every violation with a CALLERROR |

Lenient mode sorts violations by JSON-Schema keyword. A fixed set of soft keywords
(`enum`, bounds, length and item counts, `additionalProperties`, `uniqueItems`, ...)
is logged and passed. Everything else, including any keyword not on the list, is a
hard violation: unknown means reject. Type and format repairs (quoted numbers and
booleans, zone-less or space-separated timestamps) are applied only in this mode,
and the rest of the quirk handling (empty numbers, default for a missing property,
vendor measurands, unknown enum values) is switched on explicitly by further
options. A payload that strict mode accepts is accepted unchanged by lenient mode.

Validation can also be applied to outbound messages (`WithOutboundValidation`).

## Consequences

- A new user gets the cheapest, most permissive behaviour; the documentation
  recommends lenient mode for production and tolerant mode to survey a fleet.
- The default is not a safe setting for an untrusted network; this is documented
  in the README and in `docs/usage.md`.
- The soft list is a closed allow-list, so a future schema keyword fails safe.
- Strict/lenient consistency and idempotence are property-tested
  (`FuzzValidatorInvariants`) and compared with a reference implementation
  (`FuzzLenientEquivalence`).

## Alternatives considered

- Strict by default: spec-conformant but breaks many deployed chargers on first
  connect.
- Lenient by default: the better production setting, but it changes payloads
  silently; keeping it opt-in makes the repair visible in configuration.
