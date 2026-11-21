module github.com/shiv3/gocpp/cmd/gocppctl

go 1.26.0

require (
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/redis/go-redis/v9 v9.20.1
	github.com/shiv3/gocpp v0.2.0
	github.com/shiv3/gocpp/addons/registry-redis v0.0.0
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
)

replace (
	github.com/shiv3/gocpp => ../..
	github.com/shiv3/gocpp/addons/registry-redis => ../../addons/registry-redis
)
