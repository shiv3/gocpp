package csms

import (
	"context"
	"crypto/tls"
	"net/http"
	"sync"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// Conn is a CSMS-side connection to a charge point.
type Conn struct {
	inner *dispatcher.Conn

	// gone is closed once the server has finished tearing this connection down:
	// disconnect callback fired and the connection removed from the registry. Nil
	// for the short-lived wrappers handed to typed handlers, which only wait for
	// the inner connection.
	gone chan struct{}

	// disconnectOnce makes the disconnect notification fire exactly once, whether
	// it comes from a replacement or from the connection's own teardown. Do blocks
	// concurrent callers until the first call returns, which orders the
	// replacement's connect notification after the old disconnect.
	disconnectOnce sync.Once
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

// Close closes this charge point's connection from the server side: it sends a
// WebSocket close frame with status 1000 (normal closure) and the reason "closed
// by the server", fails pending calls, and waits until the teardown is complete
// (the OnDisconnect callback has run and Server.Get no longer returns the
// connection) or ctx ends, whichever comes first. The disconnect callback fires
// exactly once, with the cause ocppj.ErrConnClosedByServer; pending calls fail
// with the same error. The charge point may reconnect right away.
//
// Close is safe to call concurrently and repeatedly. If ctx ends first it
// returns ctx.Err() and the teardown still completes in the background.
func (c *Conn) Close(ctx context.Context) error {
	c.inner.Abort(ocppj.ErrConnClosedByServer)
	done := make(chan struct{})
	go func() {
		_ = c.inner.Close(nil)
		if c.gone != nil {
			<-c.gone
		}
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
