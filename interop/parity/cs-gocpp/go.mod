module github.com/shiv3/gocpp/interop/parity/cs-gocpp

go 1.26.0

replace github.com/shiv3/gocpp => ../../..

replace github.com/shiv3/gocpp/interop/parity/common => ../common

replace github.com/shiv3/gocpp/addons/link-grpc => ../../../addons/link-grpc

replace github.com/shiv3/gocpp/addons/registry-redis => ../../../addons/registry-redis

require (
	github.com/redis/go-redis/v9 v9.23.0
	github.com/shiv3/gocpp v0.3.0
	github.com/shiv3/gocpp/addons/link-grpc v0.0.0-00010101000000-000000000000
	github.com/shiv3/gocpp/addons/registry-redis v0.0.0-00010101000000-000000000000
	github.com/shiv3/gocpp/interop/parity/common v0.0.0-00010101000000-000000000000
	google.golang.org/grpc v1.85.0
)

require (
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coder/websocket v1.8.15 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/shopspring/decimal v1.4.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)
