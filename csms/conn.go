package csms

import (
	"context"
	"crypto/tls"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
)

// Conn is a CSMS-side connection to a charge point.
type Conn struct {
	inner *dispatcher.Conn

	// remoteID and srv are set (and inner is nil) on a handle for a charge point
	// held by another instance, returned by Server.Lookup; calls on it are
	// forwarded through the message router.
	remoteID string
	srv      *Server

	// gw is the gateway session this Conn belongs to: its own on a gateway, the
	// session of a handle or message on a processing tier (see GatewaySession).
	gw *gwSession

	// gone is closed once the server has finished tearing this connection down:
	// disconnect callback fired and the connection removed from the registry. Nil
	// for the short-lived wrappers handed to typed handlers, which only wait for
	// the inner connection.
	gone chan struct{}

	// session is the registry record of a remote handle (see Server.Lookup).
	session *storage.Session

	// disconnectOnce makes the disconnect notification fire exactly once, whether
	// it comes from a replacement or from the connection's own teardown. Do blocks
	// concurrent callers until the first call returns, which orders the
	// replacement's connect notification after the old disconnect.
	disconnectOnce sync.Once
}

// ID returns the charge point identifier.
func (c *Conn) ID() string {
	if c.inner == nil {
		return c.remoteID
	}
	return c.inner.ID()
}

// Remote reports whether the Conn stands for a charge point connected to another
// instance (see Server.Lookup) rather than a live local connection. Connection
// details such as Version, RemoteAddr and TLS are empty for it.
func (c *Conn) Remote() bool { return c.inner == nil }

// Session returns the registry record of this connection: for a live local
// connection the session this server claimed for it, for a remote handle the
// record that Lookup found. ok is false when the server runs without a session
// registry (and clustering), and for a connection accepted while the registry
// was unreachable until it has been claimed (Epoch is 0 until then). The value
// is a snapshot; the epoch of a connection can change when the server re-claims
// it.
func (c *Conn) Session() (s storage.Session, ok bool) {
	if c.inner == nil && c.gw != nil {
		return c.gw.storageSession(), true
	}
	if c.inner == nil {
		if c.session == nil {
			return storage.Session{}, false
		}
		return *c.session, true
	}
	p, _ := c.inner.Attachment().(*sessionRef)
	if p == nil {
		return storage.Session{}, false
	}
	return p.s, true
}

// ErrAttrsTooLarge is returned by SetAttrs when the attributes would exceed
// storage.MaxAttrBytes.
var ErrAttrsTooLarge = errors.New("csms: session attributes exceed the size cap")

// Attrs returns a copy of the session attributes (see WithSessionAttributes and
// SetAttrs); nil when there are none. They are stored in the session record as
// Meta keys with the reserved prefix "attr.".
func (c *Conn) Attrs() map[string]string {
	s, _ := c.Session()
	return s.Attrs()
}

// SetAttrs merges attrs into the session attributes: a key replaces its value, a
// key with an empty value is deleted, other attributes stay. The total size of
// keys and values is capped (storage.MaxAttrBytes; ErrAttrsTooLarge). With a
// session registry the record is updated there too (compare-and-set on the
// epoch); the local value is updated first, so a registry error leaves the next
// reconcile pass to repair the record. Attribute keys starting with "gocpp." are
// reserved for a gateway to interpret (gocpp.busy) and are stored like any other.
// Not supported on a remote handle (ErrRemoteConn).
func (c *Conn) SetAttrs(ctx context.Context, attrs map[string]string) error {
	if c.inner == nil && c.gw != nil && c.srv != nil && c.srv.backend != nil {
		return c.srv.setAttrsGateway(ctx, c, attrs)
	}
	if c.inner == nil {
		return ErrRemoteConn
	}
	ref, _ := c.inner.Attachment().(*sessionRef)
	if ref == nil {
		ref = &sessionRef{s: storage.Session{CPID: c.inner.ID()}, srv: c.srv}
	}
	s := ref.s
	meta, ok := storage.MergeAttrs(s.Meta, attrs)
	if !ok {
		return ErrAttrsTooLarge
	}
	s.Meta = meta
	c.inner.SetAttachment(&sessionRef{s: s, srv: ref.srv})
	if ref.srv == nil || ref.srv.sessions == nil || s.Epoch == 0 {
		return nil
	}
	_, err := ref.srv.sessions.reg.SetAttrs(ctx, s.CPID, s.Epoch, attrs)
	return err
}

// sessionHolder implementation, used by the session manager.

func (c *Conn) holderID() string { return c.inner.ID() }

func (c *Conn) holderSession() storage.Session {
	s, _ := c.Session()
	return s
}

func (c *Conn) setHolderSession(s storage.Session) {
	var srv *Server
	if ref, _ := c.inner.Attachment().(*sessionRef); ref != nil {
		srv = ref.srv
	}
	c.inner.SetAttachment(&sessionRef{s: s, srv: srv})
}

// sessionRef is what the server hangs on a dispatcher connection: the session
// snapshot and the server that owns it.
type sessionRef struct {
	s   storage.Session
	srv *Server
}

func (c *Conn) alive() bool { return c.inner.Context().Err() == nil }

func (c *Conn) displace() { c.inner.Abort(ocppj.ErrConnReplaced) }

// Version returns the negotiated OCPP version.
func (c *Conn) Version() ocppj.Version {
	if c.inner == nil {
		if c.gw != nil {
			return c.gw.version
		}
		return ""
	}
	return c.inner.Version()
}

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *Conn) Subprotocol() string {
	if c.inner == nil {
		if c.gw != nil {
			return c.gw.subprotocol()
		}
		return ""
	}
	return c.inner.Subprotocol()
}

// RemoteAddr returns the peer network address from the HTTP upgrade request.
func (c *Conn) RemoteAddr() string {
	if c.inner == nil {
		if c.gw != nil && c.gw.info() != nil {
			return c.gw.info().RemoteAddr
		}
		return ""
	}
	return c.inner.RemoteAddr()
}

// RequestHeader returns a copy of the HTTP upgrade request headers.
func (c *Conn) RequestHeader() http.Header {
	if c.inner == nil {
		if c.gw != nil && c.gw.info() != nil {
			return c.gw.info().RequestHeader.Clone()
		}
		return nil
	}
	return c.inner.RequestHeader()
}

// TLS returns a copy of the HTTP upgrade TLS connection state, if available.
func (c *Conn) TLS() *tls.ConnectionState {
	if c.inner == nil {
		if c.gw != nil {
			return c.gw.tlsState()
		}
		return nil
	}
	return c.inner.TLS()
}

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
	if c.inner == nil {
		if c.gw != nil && c.srv != nil && c.srv.backend != nil {
			return c.srv.closeGateway(ctx, c)
		}
		return ErrRemoteConn
	}
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

// ConnStats is a snapshot of a connection's activity; see Conn.Stats.
type ConnStats = dispatcher.ConnStats

// Stats returns a snapshot of the connection's activity: when it connected, the
// last inbound activity (any frame, WebSocket ping and pong included), the last
// outbound write, and the number of CALLs awaiting a response. It is the zero
// value for a remote handle (see Remote).
func (c *Conn) Stats() ConnStats {
	if c.inner == nil {
		return ConnStats{}
	}
	return c.inner.Stats()
}

// LastActivity returns the time of the last inbound activity on the connection
// (a data frame, WebSocket ping or pong), for idle detection beyond the built-in
// read timeout. The zero time for a remote handle.
func (c *Conn) LastActivity() time.Time { return c.Stats().LastReceivedAt }

// Done returns a channel closed when the connection ends (a remote handle's is
// never closed). Handler contexts are not cancelled by a disconnect; a handler
// that should stop when the charge point goes away selects on it.
func (c *Conn) Done() <-chan struct{} {
	if c.inner == nil || c.inner.Context() == nil {
		return nil
	}
	return c.inner.Context().Done()
}

// ConnFromContext returns the connection serving an inbound handler from the
// handler's ctx. ok is false outside a handler context.
func ConnFromContext(ctx context.Context) (*Conn, bool) {
	dc, ok := dispatcher.ConnFromContext(ctx)
	if !ok {
		c, ok := ctx.Value(gwConnKey{}).(*Conn)
		return c, ok
	}
	return &Conn{inner: dc}, true
}
