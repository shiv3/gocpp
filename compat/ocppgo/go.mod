module github.com/lorenzodonini/ocpp-go

go 1.26.0

require (
	github.com/Shopify/toxiproxy v2.1.4+incompatible
	github.com/coder/websocket v1.8.15
	github.com/go-playground/universal-translator v0.16.0
	github.com/gorilla/mux v1.8.1
	github.com/gorilla/websocket v1.5.3
	github.com/relvacode/iso8601 v1.6.0
	github.com/shiv3/gocpp v0.3.3
	github.com/sirupsen/logrus v1.4.2
	github.com/stretchr/testify v1.12.1
	go.opentelemetry.io/otel/trace v1.47.0
	gopkg.in/go-playground/validator.v9 v9.30.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/go-playground/locales v0.12.1 // indirect
	github.com/konsorten/go-windows-terminal-sequences v1.0.1 // indirect
	github.com/leodido/go-urn v1.1.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	gopkg.in/go-playground/assert.v1 v1.2.1 // indirect
)

replace github.com/shiv3/gocpp => ../..
