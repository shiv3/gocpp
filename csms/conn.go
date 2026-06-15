package csms

import (
	"crypto/tls"
	"net/http"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// Conn is a CSMS-side connection to a charge point.
type Conn struct {
	inner *dispatcher.Conn
}

// ID returns the charge point identifier.
func (c *Conn) ID() string { return c.inner.ID() }

// Version returns the negotiated OCPP version.
func (c *Conn) Version() ocppj.Version { return c.inner.Version() }

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *Conn) Subprotocol() string { return c.inner.Subprotocol() }

// RemoteAddr returns the peer network address from the HTTP upgrade request.
func (c *Conn) RemoteAddr() string { return c.inner.RemoteAddr() }

// RequestHeader returns a copy of the HTTP upgrade request headers.
func (c *Conn) RequestHeader() http.Header { return c.inner.RequestHeader() }

// TLS returns a copy of the HTTP upgrade TLS connection state, if available.
func (c *Conn) TLS() *tls.ConnectionState { return c.inner.TLS() }
