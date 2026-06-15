#!/usr/bin/env bash
set -euo pipefail
# Run benchmarks repeatedly and emit a file consumable by benchstat.
COUNT="${COUNT:-6}"
OUT="${1:-new.txt}"
go test -run '^$' -bench 'BenchmarkBootNotification|BenchmarkCallRTT' -benchmem -count "${COUNT}" ./benchmarks/ | tee "${OUT}"
