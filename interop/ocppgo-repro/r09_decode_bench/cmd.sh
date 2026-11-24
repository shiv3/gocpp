go test $GOFLAGS_REPRO -run '^$' -bench . -benchmem -benchtime 2s -cpu 1 ./r09_decode_bench
