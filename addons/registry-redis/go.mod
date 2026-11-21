module github.com/shiv3/gocpp/addons/registry-redis

go 1.26.0

require (
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/coder/websocket v1.8.15
	github.com/redis/go-redis/v9 v9.20.1
	github.com/shiv3/gocpp v0.2.0
	github.com/stretchr/testify v1.12.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/text v0.44.0 // indirect
)

// The SessionRegistry interface is not in a release yet. Drop this once the core
// module that contains it is tagged.
replace github.com/shiv3/gocpp => ../..
