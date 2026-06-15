.PHONY: schemas-fetch codegen changelog-21 test test-race lint tidy

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

lint:
	golangci-lint run

tidy:
	go mod tidy
