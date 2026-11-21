module github.com/shiv3/gocpp/addons/rollout

go 1.26.0

require (
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/common v0.66.1
	github.com/shiv3/gocpp v0.2.0
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v3 v3.0.1
)

require (
	github.com/kr/pretty v0.3.1 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/oklog/ulid/v2 v2.1.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
)

replace github.com/shiv3/gocpp => ../..
