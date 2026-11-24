# Maintaining compat/ocppgo

## Required checks (branch protection)

CI jobs in `.github/workflows/ci.yml` that guard this module. The repository
settings are not changed by the repository itself; an administrator adds these
as required status checks on `main` (Settings, Branches, branch protection rule
for `main`, "Require status checks to pass before merging"):

| Check name | What it guarantees |
|---|---|
| `ocppgo-compat` | builds; vendored files are exactly the pinned upstream tag plus `patches/`; ocpp-go's own 1.6 and 2.0.1 suites on both runtimes; ocppj suite; ws black-box suite; end-to-end tests with real WebSockets under `-race` |
| `ocppgo-apidiff` | the exported API of every upstream package is identical to the pinned tag (any line of `apidiff` output fails), except the lines in `scripts/apidiff-allow.txt` |
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

## Intentional API deviations (schema conformance)

A struct of upstream that contradicts the OCA JSON schemas may be changed here, but
only with all of: the change as a patch record (`scripts/refresh-patches.sh`), one
line per `apidiff` output line in `scripts/apidiff-allow.txt` with a comment naming
the schema property or upstream issue, a round-trip test against the schema
(`internal/schematest`), and an entry in README, "Breaking changes vs upstream
v0.19.0 (schema conformance)", saying how calling code migrates. When moving the
pin, drop the entries and patches that the new upstream tag fixes itself.

## Moving the upstream pin

`UPSTREAM` names the pinned tag and commit. The weekly workflow
`ocppgo-upstream-sync.yml` (script `scripts/upstream-sync-pr.sh`) looks for a newer
tag of the same API line (for `v0.19.0`: `v0.19.*`) of upstream `lorenzodonini/ocpp-go`, re-creates the
vendored files from it with `patches/` applied and opens a PR with the diff
statistics; if the patches do not apply or the result does not build or does not
match the API, it opens an issue with the log. A new API line (`v0.20`) is never
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

## One source tree

The module tracks one API line: upstream `lorenzodonini/ocpp-go` `v0.19`. Go maps a
module path to one tree per tag, so the tree has its own `go.mod` (module path
`github.com/lorenzodonini/ocpp-go`, which Go requires of a replacement), its own
`UPSTREAM` pin, `patches/` and tags (`compat/ocppgo/v0.19.0-gocpp.N`). Tags of the
earlier layout (`compat/ocppgo/v0.17.2-gocpp.1`, `compat/ocppgo-v019/v0.19.0-gocpp.1`)
stay on the Go proxy but are superseded and not maintained.
