module github.com/shiv3/gocpp/interop/parity/cs-ocppgo-upstream

go 1.22

replace github.com/shiv3/gocpp/interop/parity/common => ../common

require (
	github.com/gorilla/websocket v1.5.3
	github.com/lorenzodonini/ocpp-go v0.19.0
	github.com/shiv3/gocpp/interop/parity/common v0.0.0-00010101000000-000000000000
)

require (
	github.com/go-playground/locales v0.12.1 // indirect
	github.com/go-playground/universal-translator v0.16.0 // indirect
	github.com/gorilla/mux v1.8.1 // indirect
	github.com/leodido/go-urn v1.1.0 // indirect
	github.com/relvacode/iso8601 v1.6.0 // indirect
	gopkg.in/go-playground/validator.v9 v9.30.0 // indirect
)
