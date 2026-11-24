# 0005. The ocpp-go drop-in behaves like upstream by default

Status: Accepted
Date: 2026-11-25

## Context

`compat/ocppgo` exposes the public API of `lorenzodonini/ocpp-go` v0.19.0 on the
gocpp runtime, so an application can switch with a `replace` line. Applications
depend on upstream's observable behaviour, including its quirks (timing of
callbacks, which side wins a duplicate connection, validation messages), and some
of that behaviour is what the application's chargers have been tuned against.

## Decision

- The exported API of every package is identical to the pinned upstream tag; the
  `ocppgo-apidiff` CI job fails on any difference. Extra functionality lives only
  in the added package `compat`.
- By default the module behaves as upstream does wherever an application can
  observe it. A different behaviour is an opt-in function in `compat` (for
  example `SetDuplicatePolicy`, `SetMaxMessageSize`, `SetValidationLeniency`,
  `SetValidationErrorToCallback`), listed in the options table of
  `compat/ocppgo/README.md`.
- The exceptions, on by default because they remove crashes and hangs: an
  application handler or callback that panics is recovered and answered with
  `InternalError`; deadlocks and hangs (`Stop` twice, `Pause`, send on a closed
  channel) are fixed; a response is never delivered to a callback other than the
  one that sent the request.
- Vendored upstream files are the pinned tag plus recorded patches
  (see `compat/ocppgo/MAINTAINING.md`); CI checks that they match, and
  upstream's own test suites run on both runtimes. A weekly job looks for a newer
  tag of the pinned line.

## Consequences

- Migrating is a one-line change with unchanged behaviour; hardening is adopted
  one option at a time.
- The drop-in keeps upstream limits unless asked otherwise (for example no inbound
  size limit), which the native API does not: native defaults are the safe ones
  (1 MiB frames, last connection wins).
- New behaviour in the drop-in needs a `compat` option and a README row; the
  native packages (`csms`, `cp`) are where new defaults are decided.
- `OCPPGO_COMPAT_RUNTIME=legacy` runs upstream's original code path as an
  operational fallback.

## Alternatives considered

- Fixing upstream's behaviour by default: safer, but silently changes what
  deployed applications observe.
- A fork with its own API: no migration path for existing code.
