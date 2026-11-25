# 0002. Responses are matched to calls by message id

Status: Accepted
Date: 2026-11-25

## Context

OCPP-J identifies a response by the `MessageId` of the CALL it answers. A library
that instead matches by arrival order, or keeps a process-wide request queue, can
hand a response to the wrong callback after a timeout, a reconnect or a
reordering, and the result type is then wrong as well
([ocpp-go-structural-issues.md](../ocpp-go-structural-issues.md), section 3).

## Decision

Each connection owns a `pendingStore` keyed by message id. `Call` registers the
id before the frame is written, and the reader resolves CALLRESULT, CALLERROR and
(2.1) CALLRESULTERROR frames by looking the id up. A frame whose id matches no
pending call (a late answer after a timeout, or an unsolicited one) is dropped.
The response type is fixed by the type parameters of the generated message
(`csms.Call[Req, Resp]`), so there is no type assertion on the result.

By default at most one CALL per connection is outstanding, as OCPP-J requires of
each side (`callSlot`, a per-connection FIFO). `WithConcurrentCalls` lifts that
and the store then holds several entries. Only CALLs take the slot; responses and
SEND go straight to the writer.

When a connection ends, every pending call is failed with the cause; a call
timeout resolves the entry with `ErrCallTimeout`, and the timer is stopped when a
response wins.

## Consequences

- A response can never reach a callback other than the one that sent the CALL,
  whatever the order of completion.
- Message ids must be unique per connection among pending calls; generated ids
  are, and `MessageIDPolicy` governs inbound CALLs whose id is empty or longer
  than 36 characters.
- The store keeps the common single-pending case in an inline field and allocates
  a map only when a second call is pending (see `core/dispatcher/pending.go`).

## Alternatives considered

- FIFO matching (response N answers request N): cheap, but wrong as soon as one
  answer is lost or arrives late.
- One global callback table keyed by (charge point, id): needs a global lock on
  the hot path; per-connection state avoids it.
