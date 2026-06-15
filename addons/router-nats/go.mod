module github.com/shiv3/gocpp/addons/router-nats

go 1.26.0

replace github.com/shiv3/gocpp => ../..

require (
	github.com/nats-io/nats.go v1.52.0
	github.com/shiv3/gocpp v0.0.0
)

require (
	github.com/klauspost/compress v1.18.5 // indirect
	github.com/nats-io/nkeys v0.4.15 // indirect
	github.com/nats-io/nuid v1.0.1 // indirect
	golang.org/x/crypto v0.49.0 // indirect
	golang.org/x/sys v0.45.0 // indirect
)
