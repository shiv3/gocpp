# Maintaining compat/ocppgo

## Required checks (branch protection)

CI jobs in `.github/workflows/ci.yml` that guard this module. The repository
settings are not changed by the repository itself; an administrator adds these
as required status checks on `main` (Settings, Branches, branch protection rule
for `main`, "Require status checks to pass before merging"):

| Check name | What it guarantees |
|---|---|
| `ocppgo-compat` | builds; vendored files are exactly the pinned upstream tag plus `patches/`; ocpp-go's own 1.6 and 2.0.1 suites on both runtimes; ocppj suite; ws black-box suite; end-to-end tests with real WebSockets under `-race` |
| `ocppgo-apidiff` | the exported API of every upstream package is identical to the pinned tag (any line of `apidiff` output fails) |
| `ocppgo-license` | `LICENSE` equals upstream's, `NOTICE` and per-file headers in order, dependency allow list (`scripts/licenses.allow`) |
| `apidiff` (the repository wide one in `apidiff.yml`) | gocpp's own API gate (a no-op before v1.0, but it reports, so it can be required) |

Also useful on the same rule: "Require branches to be up to date before merging"
(the jobs compare against a fixed pin, so this is only needed when several
compat PRs are open), and "Do not allow bypassing".

The jobs run on every pull request (they have no path filter, so a required
check is never "pending forever"). The scheduled workflow
`ocppgo-upstream-sync.yml` opens PRs from a branch created with the default
`GITHUB_TOKEN`; PRs opened that way do not trigger workflows, so a maintainer
closes and reopens the PR (or pushes an empty commit) to run the required checks.
"Allow GitHub Actions to create and approve pull requests" must be enabled in
Settings, Actions, General for the sync PRs to be created at all; without it the
workflow log shows the error and the pin can still be moved by hand.

## Moving the upstream pin

`UPSTREAM` names the pinned tag and commit. The weekly workflow
`ocppgo-upstream-sync.yml` (script `scripts/upstream-sync-pr.sh`) looks for a newer
tag of the same API line (for `v0.17.2-fix.3`: `v0.17.*`), re-creates the
vendored files from it with `patches/` applied and opens a PR with the diff
statistics; if the patches do not apply or the result does not build or does not
match the API, it opens an issue with the log. A new API line (`v0.18`) is never
synced automatically.

To do it by hand: edit `UPSTREAM`, run `scripts/sync-upstream.sh`, fix rejected
patches and run `scripts/refresh-patches.sh`, then `scripts/apidiff.sh`.

## Releasing

The module is released with nested-module tags
`compat/ocppgo/v<API version>-gocpp.<n>`. Each release requires a tagged root
`github.com/shiv3/gocpp` version that contains every core API the module uses;
tag the root first, then update the `require` in `go.mod` (the module is
developed against the checkout through a `replace` directive that consumers do
not see), then tag the module. Pre-release tags (`-rc.N`) are for proxy checks.

## Two source trees

`compat/ocppgo` implements the `v0.17.2-fix.x` API line (the fork) and
`compat/ocppgo-v019` the upstream `v0.19` line. A Go module path maps to one tree
per tag, and the two APIs are not compatible, so each line is a directory with its
own `go.mod` (module path `github.com/lorenzodonini/ocpp-go` in both), its own
`UPSTREAM` pin, patches and tags (`compat/ocppgo/v0.17.2-gocpp.N`,
`compat/ocppgo-v019/v0.19.0-gocpp.N`). A branch per line was rejected: CI, the
required checks and the shared gocpp core changes would then live on different
branches. The runtime files (`ocppj/server_gocpp.go`, `ws/server.go`,
`ws/admission.go`, `compat/`, `internal/compatrt/`, the `e2e` tests) exist in both
trees and differ only where the upstream API differs; a change to one is made in
the other in the same PR (`diff -r` of the two directories shows the drift). The
required checks of the second tree are the same set with the prefix
`ocppgo-v019-`.
