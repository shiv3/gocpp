module github.com/lorenzodonini/ocpp-go

go 1.26.0

require (
	github.com/Shopify/toxiproxy v2.1.4+incompatible
	github.com/coder/websocket v1.8.15
	github.com/go-playground/universal-translator v0.18.1
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/relvacode/iso8601 v1.6.0
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.3
	github.com/shiv3/gocpp v0.4.0
	github.com/sirupsen/logrus v1.9.3
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/otel/trace v1.47.0
	gopkg.in/go-playground/validator.v9 v9.31.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.9 // indirect
	github.com/go-playground/locales v0.14.1 // indirect
	github.com/leodido/go-urn v1.1.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
)

replace github.com/shiv3/gocpp => ../..

// The tag compat/ocppgo/v0.17.2-gocpp.1 predates the v0.19.0 line, is not
// maintained and is superseded by compat/ocppgo v0.19.0-gocpp.x. It cannot be
// deleted from the module proxy; retracting it hides it from `go list -m
// -versions` and keeps `@latest` away from it. Takes effect from the next
// compat/ocppgo tag.
retract v0.17.2-gocpp.1
