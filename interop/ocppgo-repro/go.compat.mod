module github.com/shiv3/gocpp/interop/ocppgo-repro

go 1.26.0

require (
	github.com/gorilla/websocket v1.5.3
	github.com/lorenzodonini/ocpp-go v0.19.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/go-playground/universal-translator v0.18.1 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/leodido/go-urn v1.1.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/relvacode/iso8601 v1.6.0 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3 // indirect
	github.com/shiv3/gocpp v0.4.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	gopkg.in/go-playground/validator.v9 v9.31.0 // indirect
)

replace github.com/lorenzodonini/ocpp-go => github.com/shiv3/gocpp/compat/ocppgo v0.19.0-gocpp.3
