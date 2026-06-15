#!/usr/bin/env bash
# Fetch OCA OCPP JSON schemas into schemas/<version>/.
# OCA distributes schemas inside the spec zip; this script expects the zip URL(s)
# to be provided via env vars, then extracts the *.json schema files.
set -euo pipefail

OUT_DIR="${OUT_DIR:-schemas}"

fetch() {
  local version="$1" url="$2"
  local dir="${OUT_DIR}/${version}"
  mkdir -p "${dir}"
  local tmp
  tmp="$(mktemp -d)"
  echo "Fetching ${version} schemas from ${url}"
  curl -fsSL "${url}" -o "${tmp}/schemas.zip"
  unzip -o -j "${tmp}/schemas.zip" '*.json' -d "${dir}"
  rm -rf "${tmp}"
  echo "Extracted $(ls "${dir}" | wc -l | tr -d ' ') files to ${dir}"
}

# URLs are not committed (OCA distribution terms vary); supply at run time:
#   V16_SCHEMA_URL=... V201_SCHEMA_URL=... V21_SCHEMA_URL=... make schemas-fetch
[ -n "${V16_SCHEMA_URL:-}" ]  && fetch v16  "${V16_SCHEMA_URL}"
[ -n "${V201_SCHEMA_URL:-}" ] && fetch v201 "${V201_SCHEMA_URL}"
[ -n "${V21_SCHEMA_URL:-}" ]  && fetch v21  "${V21_SCHEMA_URL}"
echo "Done. Review 'git diff schemas/' before committing."
