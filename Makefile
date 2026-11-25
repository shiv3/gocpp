.PHONY: schemas-fetch codegen changelog-21 test test-race fuzz lint tidy interop

schemas-fetch:
	./scripts/fetch-schemas.sh

changelog-21:
	go run ./internal/codegen/cmd/diff -from v201 -to v21

codegen:
	go run ./internal/codegen -version v16
	go run ./internal/codegen -version v201
	go run ./internal/codegen -version v21

test:
	go test ./...

test-race:
	go test -race ./...

# Run every fuzz target briefly (FUZZTIME per target, default 20s).
FUZZTIME ?= 20s
fuzz:
	go test -run '^$$' -fuzz '^FuzzParseEquivalence$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/ocppj
	go test -run '^$$' -fuzz '^FuzzParseRoundTrip$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/ocppj
	go test -run '^$$' -fuzz '^FuzzEncodeEquivalence$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/ocppj
	go test -run '^$$' -fuzz '^FuzzLenientEquivalence$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/schema
	go test -run '^$$' -fuzz '^FuzzDecodeEquivalence$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/schema
	go test -run '^$$' -fuzz '^FuzzValidatorInvariants$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/schema
	go test -run '^$$' -fuzz '^FuzzTypedDecode$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/schema
	go test -run '^$$' -fuzz '^FuzzConnInbound$$' -fuzztime $(FUZZTIME) -parallel 2 ./core/dispatcher

lint:
	golangci-lint run

tidy:
	go mod tidy

# Interoperability matrix against third-party OCPP implementations (docs/interop.md).
# Needs docker; pulls several GB. INTEROP_PEERS / INTEROP_VERSIONS narrow it.
interop:
	go test -tags interop -count=1 -timeout 60m -json -run 'TestInterop_Gocpp' ./interop/ > interop-results.json || true
	python3 scripts/interop-matrix.py interop-results.json --failures
