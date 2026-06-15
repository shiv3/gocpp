.PHONY: schemas-fetch codegen test test-race lint tidy

schemas-fetch:
	./scripts/fetch-schemas.sh

codegen:
	go run ./internal/codegen -version v16

test:
	go test ./...

test-race:
	go test -race ./...

lint:
	golangci-lint run

tidy:
	go mod tidy
