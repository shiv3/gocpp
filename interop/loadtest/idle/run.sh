#!/bin/bash
# Idle-connection footprint: start a parity server, open N idle OCPP websockets
# with the raw-TCP client in this directory, and print RSS / goroutines / heap
# after the connections settle.
# usage: run.sh LABEL SERVER_BIN IDLE_BIN PORT PPROF_PORT N SERVER_CPUS CLIENT_CPUS [server args...]
label=$1; bin=$2; idle=$3; port=$4; pp=$5; n=$6; scpu=$7; ccpu=$8; shift 8
ulimit -n 65536
taskset -c $scpu "$bin" -listen 127.0.0.1:$port -lite -pprof 127.0.0.1:$pp "$@" >/tmp/idle-$label.srv.log 2>&1 &
spid=$!
sleep 1
snap() {
  curl -s "http://127.0.0.1:$pp/debug/pprof/heap?gc=1&debug=1" >/dev/null
  mem=$(curl -s "http://127.0.0.1:$pp/debug/pprof/heap?gc=1&debug=1" | grep -E "^# (HeapAlloc|HeapInuse|StackInuse|Sys) " | tr '\n' ' ')
  gor=$(curl -s "http://127.0.0.1:$pp/debug/pprof/goroutine?debug=1" | head -1)
  echo "[$label/$1] rss_kb=$(grep VmRSS /proc/$spid/status | awk '{print $2}') $gor :: $mem"
}
snap base
taskset -c $ccpu "$idle" -addr 127.0.0.1:$port -path /ocpp/ -n $n -idle 8s -after 10s | while read l; do
  case "$l" in "PHASE idle") snap idle;; "PHASE hb") snap hb;; *) echo "$l";; esac
done
kill $spid
