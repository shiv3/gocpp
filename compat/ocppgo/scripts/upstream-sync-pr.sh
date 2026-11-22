#!/usr/bin/env bash
# Looks for a tag of the pinned line that is newer than UPSTREAM_TAG, re-creates
# the vendored files from it with patches/ applied, and opens a PR with the diff
# (needs gh and GH_TOKEN). When the patches do not apply, or the result does not
# build, it opens an issue instead. Does nothing when the pin is current.
#
#   scripts/upstream-sync-pr.sh [--dry-run]
#
# A "line" is the tag prefix of UPSTREAM_TAG up to the second dot (v0.17 for
# v0.17.2-fix.3): the API line this module implements. A new line (v0.18) is not
# a sync; it needs a decision and its own tree.
set -euo pipefail
here="$(cd "$(dirname "$0")/.." && pwd)"
# shellcheck source=../UPSTREAM
. "$here/UPSTREAM"
dry=0; [ "${1:-}" = "--dry-run" ] && dry=1
line="$(echo "$UPSTREAM_TAG" | sed -E 's/^(v[0-9]+\.[0-9]+)\..*/\1/')"
tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
latest="$(git ls-remote --tags "$UPSTREAM_REPO" "refs/tags/${line}.*" | awk '{print $2}' | sed 's|refs/tags/||' | grep -v '\^{}' | sort -V | tail -n1)"
echo "pinned $UPSTREAM_TAG, newest of line $line: ${latest:-none}"
if [ -z "$latest" ] || [ "$latest" = "$UPSTREAM_TAG" ] || [ "$(printf '%s\n%s\n' "$UPSTREAM_TAG" "$latest" | sort -V | tail -n1)" = "$UPSTREAM_TAG" ]; then
  echo "pin is current"; exit 0
fi
git clone -q --depth 1 --branch "$latest" "$UPSTREAM_REPO" "$tmp/up"
commit="$(git -C "$tmp/up" rev-parse 'HEAD^{commit}')"
branch="compat/upstream-sync-$latest"
body="$tmp/body.md"
report_issue() {
  { echo "Syncing compat/ocppgo to \`$latest\` failed: $1"; echo; echo '```'; tail -n 40 "$tmp/log"; echo '```'; } > "$body"
  if [ "$dry" = 1 ]; then cat "$body"; exit 1; fi
  gh issue create --title "compat/ocppgo: upstream $latest does not sync" --body-file "$body" || true
  exit 1
}
sed -i "s|^UPSTREAM_TAG=.*|UPSTREAM_TAG=$latest|; s|^UPSTREAM_COMMIT=.*|UPSTREAM_COMMIT=$commit|" "$here/UPSTREAM"
if ! "$here/scripts/sync-upstream.sh" "$tmp/up" >"$tmp/log" 2>&1; then report_issue "patches/ do not apply"; fi
# The synced tree must still build and keep the same exported API as the new pin.
if ! (cd "$here" && go build ./... && scripts/apidiff.sh "$tmp/up") >>"$tmp/log" 2>&1; then report_issue "build or apidiff failed"; fi
{
  echo "Moves the pin of compat/ocppgo from \`$UPSTREAM_TAG\` to \`$latest\` (\`$commit\`)."
  echo
  echo "Upstream changes between the tags:"
  echo '```'
  (git clone -q --filter=blob:none "$UPSTREAM_REPO" "$tmp/full" && git -C "$tmp/full" diff --stat "$UPSTREAM_TAG" "$latest" | tail -n 40) 2>/dev/null || echo "(diff unavailable)"
  echo '```'
  echo
  echo "Patches applied cleanly; \`go build\` and \`scripts/apidiff.sh\` pass. CI runs the suites in both runtime modes. After merging, a maintainer tags \`compat/ocppgo/${latest}-gocpp.N\`."
} > "$body"
if [ "$dry" = 1 ]; then cat "$body"; git -C "$here" checkout -q -- . ; exit 0; fi
cd "$here/../.."
git config user.name "github-actions[bot]"; git config user.email "41898282+github-actions[bot]@users.noreply.github.com"
git checkout -q -b "$branch"
git add -A compat/ocppgo
git commit -q -m "compat/ocppgo: sync to upstream $latest"
git push -q origin "$branch"
gh pr create --title "compat/ocppgo: sync to upstream $latest" --body-file "$body"
