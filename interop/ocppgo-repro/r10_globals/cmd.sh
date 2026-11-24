go run $GOFLAGS_REPRO ./r10_globals
for t in TestLifecycleUnderRaceDetector TestSecondServerSetupWhileFirstServes; do
  echo
  echo "--- go test -race -run $t ---"
  go test $GOFLAGS_REPRO -race -count=1 -timeout 60s -run "^$t\$" ./r10_globals > /tmp/r10-race.$$ 2>&1
  echo "go test exit status: $?; data races reported: $(grep -c 'WARNING: DATA RACE' /tmp/r10-race.$$)"
  grep -A2 -E '^(Write|Read|Previous write|Previous read) at' /tmp/r10-race.$$ | grep -E '^\s+(github.com|ocppgo)' | sed -E 's/^\s+//' | sort | uniq -c | sort -rn | head -8
done
rm -f /tmp/r10-race.$$
