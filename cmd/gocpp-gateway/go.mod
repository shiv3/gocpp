module github.com/shiv3/gocpp/cmd/gocpp-gateway

go 1.26.0

require (
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/coder/websocket v1.8.15
	github.com/nats-io/nats-server/v2 v2.15.3
	github.com/nats-io/nats.go v1.56.0
	github.com/prometheus/client_golang v1.23.2
	github.com/redis/go-redis/v9 v9.20.1
	github.com/shiv3/gocpp v0.2.0
	github.com/shiv3/gocpp/addons/link-grpc v0.0.0
	github.com/shiv3/gocpp/addons/link-nats v0.0.0
	github.com/shiv3/gocpp/addons/registry-redis v0.0.0
	github.com/stretchr/testify v1.12.1
	google.golang.org/grpc v1.85.0
)

require (
	github.com/antithesishq/antithesis-sdk-go v0.9.0-default-no-op // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/go-jose/go-jose/v4 v4.1.4 // indirect
	github.com/google/go-tpm v0.9.8 // indirect
	github.com/klauspost/compress v1.20.4 // indirect
	github.com/minio/highwayhash v1.0.4 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/nats-io/jwt/v2 v2.8.3 // indirect
	github.com/nats-io/nkeys v0.4.17 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.59.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/time v0.16.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260928230214-8a89bd6388cc // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace (
	github.com/shiv3/gocpp => ../..
	github.com/shiv3/gocpp/addons/link-grpc => ../../addons/link-grpc
	github.com/shiv3/gocpp/addons/link-nats => ../../addons/link-nats
	github.com/shiv3/gocpp/addons/registry-redis => ../../addons/registry-redis
)
