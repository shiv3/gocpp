// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package compat

import (
	"net/http"
	"time"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
	"github.com/lorenzodonini/ocpp-go/ocppj"
	"github.com/lorenzodonini/ocpp-go/ws"
)

// ConnectionIDs returns the ids of the charge points connected to s, sorted
// (ocpp-go v0.19.0's ws.Server has no such list). It returns nil when s is not
// the module's own server.
func ConnectionIDs(s *ws.Server) []string {
	if nat, ok := native(s); ok {
		return nat.ConnectionIDs()
	}
	return nil
}

// Handler returns the websocket upgrade handler of s, for mounting on an
// external mux or framework router (gin, chi, net/http's ServeMux, ...) that
// owns the listener. The charge point id is the last segment of the request
// path, as for s.Start's listenPath, unless SetIDResolver says otherwise:
//
//	http.Handle("/ocpp/", compat.Handler(srv))
//
// Use it instead of the Start of the central system, which listens by itself: call
// StartWithoutListener once, serve Handler, and stop with the central system's Stop.
// It returns nil when s is not the module's own server.
func Handler(s *ws.Server) http.Handler {
	nat, ok := native(s)
	if !ok {
		return nil
	}
	return nat.Handler()
}

// StartWithoutListener does what Start of the ocppj server does, except that
// it does not listen: s starts accepting connections that arrive through
// Handler(srv). Stop (of the ocppj server or the central system) stops it,
// closing the connections. It returns false, and does nothing, when s runs on
// the legacy runtime (OCPPGO_COMPAT_RUNTIME=legacy, or an injected dispatcher) or
// srv is not the module's own server.
func StartWithoutListener(s *ocppj.Server, srv *ws.Server) bool {
	c, ok := control(s)
	if !ok {
		return false
	}
	nat, ok := native(srv)
	if !ok {
		return false
	}
	c.StartExternal(nat)
	return true
}

// SetBasicAuthIDHandler requires HTTP Basic Authentication like
// ws.Server.SetBasicAuthHandler, with the charge point id in the call; a refusal
// answers 401 with the WWW-Authenticate challenge. When both are set, both must
// accept. Call it before Start. It returns false when s is not the module's own server.
func SetBasicAuthIDHandler(s *ws.Server, h func(chargePointID, username, password string) bool) bool {
	nat, ok := native(s)
	if !ok {
		return false
	}
	nat.SetBasicAuthIDHandler(h)
	return true
}

// SetIDResolver derives the charge point id from the upgrade request, for
// paths such as "/{organization}/{chargepoint}": pathID is the last path
// segment (ocpp-go's id); return the id to use, or false to refuse the upgrade
// with 404. The id is what the check client handler, basic auth, the
// application and the connection table see. Call it before Start.
func SetIDResolver(s *ws.Server, f func(r *http.Request, pathID string) (id string, ok bool)) bool {
	nat, ok := native(s)
	if !ok {
		return false
	}
	nat.SetIDResolver(f)
	return true
}

// Compression selects permessage-deflate for the connections of a server.
type Compression int

const (
	// CompressionDisabled never negotiates compression (the default, as ocpp-go v0.19.0).
	CompressionDisabled Compression = iota
	// CompressionContextTakeover compresses with a window kept for the connection (best ratio, most memory).
	CompressionContextTakeover
	// CompressionNoContextTakeover compresses each message on its own (less memory per connection).
	CompressionNoContextTakeover
)

// SetCompression lets s negotiate permessage-deflate with chargers that offer
// it. It applies to connections accepted afterwards. It returns false when s is
// not the module's own server.
func SetCompression(s *ws.Server, c Compression) bool {
	nat, ok := native(s)
	if !ok {
		return false
	}
	nat.SetCompression(compatrt.Compression(c))
	return true
}

// ConnTimeouts are the timeouts of one connection; zero keeps the server's.
type ConnTimeouts struct {
	// CallTimeout is how long a request sent to this charge point waits for its
	// answer (SetCallTimeout / the dispatcher's timeout for the server as a whole).
	CallTimeout time.Duration
	// IdleTimeout closes the connection when no frame arrives within it (the
	// server's ws.ServerTimeoutConfig.PingWait for the server as a whole).
	IdleTimeout time.Duration
}

// SetConnTimeouts overrides the call and idle timeouts per connection: f is
// called once for every upgrade, with the request and the charge point id. Call
// it before Start. Needs the gocpp runtime for CallTimeout; IdleTimeout works
// with either. It returns false when s is not the module's own server.
func SetConnTimeouts(s *ws.Server, f func(r *http.Request, chargePointID string) ConnTimeouts) bool {
	nat, ok := native(s)
	if !ok {
		return false
	}
	if f == nil {
		nat.SetConnTimeouts(nil)
		return true
	}
	nat.SetConnTimeouts(func(r *http.Request, id string) compatrt.ConnTimeouts {
		t := f(r, id)
		return compatrt.ConnTimeouts{CallTimeout: t.CallTimeout, IdleTimeout: t.IdleTimeout}
	})
	return true
}
