# Builds the three parity CSMS binaries into bin/ (needs the Go toolchain on PATH).
GO ?= go

.PHONY: all clean vet
all: bin/cs-gocpp bin/cs-ocppgo-upstream bin/cs-ocppgo-compat

bin/cs-gocpp: $(shell find cs-gocpp common -name '*.go' -o -name go.mod)
	cd cs-gocpp && $(GO) build -o ../bin/cs-gocpp .

bin/cs-ocppgo-upstream: $(shell find cs-ocppgo-upstream common -name '*.go' -o -name go.mod)
	cd cs-ocppgo-upstream && $(GO) build -o ../bin/cs-ocppgo-upstream .

bin/cs-ocppgo-compat: $(shell find cs-ocppgo-compat common ../../compat/ocppgo -name '*.go' -o -name go.mod)
	cd cs-ocppgo-compat && $(GO) build -o ../bin/cs-ocppgo-compat .

vet:
	cd common && $(GO) vet ./...
	cd cs-gocpp && $(GO) vet ./...
	cd cs-ocppgo-upstream && $(GO) vet ./...
	cd cs-ocppgo-compat && $(GO) vet ./...

clean:
	rm -rf bin
