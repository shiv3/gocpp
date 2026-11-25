# Interoperability testing

gocpp is tested against independent OCPP implementations in both directions: its
CSMS with a peer acting as charge point, and its charge point with a peer acting
as CSMS, for OCPP 1.6J, 2.0.1 and 2.1 wherever the peer supports the version.
Not part of per-PR CI (it pulls several GB of images): run it from the
`Interop` workflow (workflow_dispatch, also weekly on Mondays) or locally.

## Peers

The peers are the containers published by [otfabric/ocpp-interop](https://github.com/otfabric/ocpp-interop)
(MIT) at pinned digests, all behind one command-line contract. The harness in
`interop/ref_*_test.go` is an independent reimplementation of the small part of
that contract gocpp needs.

| peer | implementation | roles |
|---|---|---|
| lorenzodonini | lorenzodonini/ocpp-go v0.19.0 (Go) | CSMS + station, 1.6, 2.0.1 |
| mobilityhouse | mobilityhouse/ocpp 2.1.0 (Python) | CSMS + station, 1.6, 2.0.1, 2.1 |
| chargetime | ChargeTimeEU/Java-OCA-OCPP v2.0 (Java) | CSMS + station, 1.6, 2.0.1 |
| everest | EVerest libocpp, everest-core 2026.02.2 (C++) | station, 1.6, 2.0.1, 2.1 |
| cbsigma | cB-Sigma/ocpp v0.2.0 (Go) | CSMS + station, 1.6, 2.0.1, 2.1 |
| ocppkit | hupe1980/ocpp-kit 0.3.0 (Rust) | CSMS + station, 1.6, 2.0.1, 2.1 |
| seanlee | seanlee0923/ocpp v0.3.0 (Go) | CSMS + station, 1.6, 2.0.1, 2.1 |
| citrineos | CitrineOS core v2.1.0 (TypeScript) | CSMS, 1.6, 2.0.1, 2.1 |

The older `citrineos_21_test.go` in this directory remains a separate smoke test.

## What is checked

- Every message of the version, in each direction: the peer sends or answers a
  fixture and gocpp must see the same payload (and the reverse).
- OCPP 2.1 SEND, in both directions.
- Unknown and unanswered actions return a CALLERROR.
- Security profiles 1-3 and `permessage-deflate` compression.

## Results

Run 2026-11-25. Counts are test cases (both directions summed, per peer and version).

| peer | OCPP | pass | fail | known peer issue | skipped (peer lacks it) |
|---|---|---:|---:|---:|---:|
| lorenzodonini | 1.6 | 105 | 0 | 0 | 0 |
| lorenzodonini | 2.0.1 | 154 | 0 | 3 | 0 |
| mobilityhouse | 1.6 | 104 | 0 | 1 | 0 |
| mobilityhouse | 2.0.1 | 157 | 0 | 0 | 0 |
| mobilityhouse | 2.1 | 206 | 0 | 2 | 3 |
| chargetime | 1.6 | 104 | 0 | 0 | 1 |
| chargetime | 2.0.1 | 156 | 0 | 1 | 0 |
| everest | 1.6 | 53 | 0 | 0 | 1 |
| everest | 2.0.1 | 79 | 0 | 0 | 1 |
| everest | 2.1 | 102 | 0 | 1 | 4 |
| cbsigma | 1.6 | 104 | 0 | 0 | 1 |
| cbsigma | 2.0.1 | 156 | 0 | 0 | 1 |
| cbsigma | 2.1 | 210 | 0 | 0 | 3 |
| ocppkit | 1.6 | 60 | 0 | 0 | 1 |
| ocppkit | 2.0.1 | 86 | 0 | 0 | 1 |
| ocppkit | 2.1 | 112 | 0 | 0 | 4 |
| seanlee | 1.6 | 80 | 0 | 0 | 25 |
| seanlee | 2.0.1 | 156 | 0 | 0 | 1 |
| seanlee | 2.1 | 208 | 0 | 0 | 3 |
| citrineos | 1.6 | 39 | 0 | 0 | 12 |
| citrineos | 2.0.1 | 72 | 0 | 0 | 5 |
| citrineos | 2.1 | 75 | 0 | 0 | 29 |

"Skipped" means the peer has no implementation of that message or role (for
example CitrineOS handles a subset of actions). No failure remains classified
as a gocpp bug after the fix below.

### Findings

| class | finding |
|---|---|
| gocpp bug (fixed, #148) | Integer fields rejected numbers such as `20000.0` that JSON Schema allows; EVerest sends `energyAmount: 20000.0` in NotifyEVChargingNeeds (2.0.1). |
| peer bug | lorenzodonini v0.19.0: GetCompositeScheduleResponse (2.0.1) lacks the OCA structure; NotifyReport drops `tbc:false`. |
| peer bug | mobilityhouse 2.1.0: SecurityEventNotification (1.6) cannot omit `techInfo`; no CALLERROR for unhandled 2.1 actions (fixed on its master). |
| peer bug | EVerest libocpp: cannot send ReportDERControl (2.1). |
| peer bug | ChargeTime Java-WebSocket 1.6.0: permessage-deflate context kept despite no-context-takeover (corrupt frames). |

These are recorded as known issues in `interop/ref_support_test.go` and show up
as skips in the run.

## Running locally

```sh
make interop                                    # everything (needs docker or podman, several GB)
INTEROP_PEERS=everest,cbsigma INTEROP_VERSIONS=2.0.1 make interop
INTEROP_DOCKER=podman make interop              # when `docker` is not the command
OCPP_INTEROP_EVEREST_IMAGE=local/everest:dev make interop   # override one image
```

`scripts/interop-matrix.py interop-results.json [--failures]` renders the
`go test -json` output as the matrix above. Run one or two peers at a time on a
small machine.
