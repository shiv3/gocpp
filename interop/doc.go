// Package interop holds tests that run gocpp against real external OCPP
// implementations (CitrineOS, SteVe, EVerest, simulators). They are build-tagged
// with `interop` and excluded from the default `go test ./...` run because they
// require Docker images / external services. Run with: go test -tags interop ./interop/
package interop
