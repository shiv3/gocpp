package linkgrpc

import "github.com/shiv3/gocpp/gateway"

// GatewayProtoForTest makes a gateway speak the protocol range r and offer caps,
// to stand in for a future build in skew tests.
func GatewayProtoForTest(r gateway.ProtoRange, caps ...string) GatewayOption {
	return func(g *Gateway) { g.proto, g.caps = r, append(append([]string{}, supportedCaps...), caps...) }
}

// BackendProtoForTest is GatewayProtoForTest for the backend end.
func BackendProtoForTest(r gateway.ProtoRange, caps ...string) BackendOption {
	return func(b *Backend) { b.proto, b.caps = r, append(append([]string{}, supportedCaps...), caps...) }
}
