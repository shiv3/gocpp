# 0003. Message types, registrations and schemas are generated from the OCA JSON schemas

Status: Accepted
Date: 2026-11-25

## Context

OCPP 1.6, 2.0.1 and 2.1 together define several hundred message and data types.
Hand-written structs drift from the published schemas, and runtime validation
needs the schemas anyway.

## Decision

`internal/codegen` reads `schemas/<version>/*.json` (the OCA schemas) and a
profile YAML per version and emits, for each of `v16`, `v201`, `v21`:

- `messages/`: one file per message, shared nested types de-duplicated, enums
  with `Valid()`, `$ref` and `definitions` resolved;
- `profiles/`: typed `ocppj.Message[Req, Resp]` values and `RegisterSchemas`;
- `schemas/`: the JSON schemas embedded with `go:embed`, used for validation.

Constraints the published schemas lack are added as JSON merge patches under
`schemas/overrides/<version>/` rather than by editing the source schemas. The
version packages depend only on `core/ocppj` and `core/schema`; the dispatcher
knows nothing about any version, so adding 2.1 added no dispatcher code.

Output is deterministic and committed. CI runs `make codegen` and fails on
`git diff`, so a schema or generator change cannot land without its regenerated
output. A weekly workflow compares the vendored schemas with the published ones.

## Consequences

- Generated files carry a "DO NOT EDIT" header; changes go to the generator, the
  profile or an override.
- Types are generated, not hand-tuned: a field that the schema makes optional is
  a pointer, which is visible in the API.
- Because the same schemas drive types and validation, a payload the strict
  validator accepts decodes into the generated type; the fuzz target
  `FuzzTypedDecode` checks this.

## Alternatives considered

- Hand-written types with struct tags only: no single source of truth for
  validation.
- Generating at build time: keeps the repository smaller but makes `go get`
  consumers depend on the generator and the schema download; committed output
  does neither.
