# Builds both parity CSMS binaries into bin/ (needs the Go toolchain on PATH).
GO ?= go

.PHONY: all clean vet test
all: bin/cs-gocpp bin/cs-ocppgo

bin/cs-gocpp: $(shell find cs-gocpp common -name '*.go' -o -name go.mod)
	cd cs-gocpp && $(GO) build -o ../bin/cs-gocpp .

bin/cs-ocppgo: $(shell find cs-ocppgo common -name '*.go' -o -name go.mod)
	cd cs-ocppgo && $(GO) build -o ../bin/cs-ocppgo .

vet:
	cd common && $(GO) vet ./...
	cd cs-gocpp && $(GO) vet ./...
	cd cs-ocppgo && $(GO) vet ./...
	cd smoke && $(GO) vet ./...

test: all
	cd smoke && PARITY_BIN_DIR=$(CURDIR)/bin $(GO) test -count=1 ./...

clean:
	rm -rf bin
