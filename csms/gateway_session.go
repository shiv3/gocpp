package csms

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
	"github.com/shiv3/gocpp/gateway"
)

// gwSession is what a Conn knows about its gateway session: on a gateway, its
// own session; on a processing tier, the session a handle or an inbound message
// refers to. Its ref and version are immutable; info and attributes are swapped atomically.
type gwSession struct {
	ref     gateway.SessionRef
	version ocppj.Version
	infoP   atomic.Pointer[gateway.SessionInfo] // nil when the open event was not seen
	attrsP  atomic.Pointer[map[string]string]   // session attributes (routing labels); see attrs()
	// routePool is the pool of the last routing decision copied to the registry
	// (Session.Meta["route.pool"]); nil until the first copy succeeded.
	routePool atomic.Pointer[string]
	// routeBusy is set while a registry write of the copy is in flight: one at a
	// time per session, so an older value cannot overtake a newer one.
	routeBusy atomic.Bool
}

func (g *gwSession) subprotocol() string {
	if g.info() != nil && g.info().Subprotocol != "" {
		return g.info().Subprotocol
	}
	if g.version == "" {
		return ""
	}
	return "ocpp" + string(g.version)
}

// storageSession is the registry-shaped view of the session.
func (g *gwSession) storageSession() storage.Session {
	s := storage.Session{
		CPID: g.ref.CPID, GatewayID: g.ref.GatewayID, Boot: g.ref.Boot, Epoch: g.ref.Epoch,
		Version: string(g.version),
	}
	if g.info() != nil {
		s.Meta = map[string]string{"remote_addr": g.info().RemoteAddr}
	}
	if p := g.routePool.Load(); p != nil {
		if s.Meta == nil {
			s.Meta = map[string]string{}
		}
		s.Meta[storage.RoutePrefix+routePoolKey] = *p
	}
	return s
}

// fromStorage rebuilds a session from a registry record.
func fromStorage(s storage.Session) *gwSession {
	return &gwSession{
		ref:     gateway.SessionRef{CPID: s.CPID, GatewayID: s.GatewayID, Boot: s.Boot, Epoch: s.Epoch},
		version: ocppj.Version(strings.TrimPrefix(s.Version, "ocpp")),
	}
}

func (g *gwSession) tlsState() *tls.ConnectionState {
	if g.info() == nil || len(g.info().PeerCerts) == 0 {
		return nil
	}
	st := &tls.ConnectionState{HandshakeComplete: true}
	for _, der := range g.info().PeerCerts {
		if c, err := x509.ParseCertificate(der); err == nil {
			st.PeerCertificates = append(st.PeerCertificates, c)
		}
	}
	return st
}

// GatewaySession returns the gateway session this Conn stands for: the
// connection's own session on a gateway, or, on a processing tier behind a
// gateway (WithGatewayLink), the session of the handle or of the message being
// handled. ok is false for an ordinary embedded connection.
func (c *Conn) GatewaySession() (ref gateway.SessionRef, ok bool) {
	if c.gw == nil {
		return gateway.SessionRef{}, false
	}
	return c.gw.ref, true
}

type gwConnKey struct{}

func withGatewayConn(ctx context.Context, c *Conn) context.Context {
	return context.WithValue(ctx, gwConnKey{}, c)
}

// peerCerts returns the DER chain of the TLS peer certificates, leaf first.
func peerCerts(r *http.Request) [][]byte {
	if r.TLS == nil {
		return nil
	}
	out := make([][]byte, 0, len(r.TLS.PeerCertificates))
	for _, c := range r.TLS.PeerCertificates {
		out = append(out, c.Raw)
	}
	return out
}

func (g *gwSession) attrs() map[string]string {
	if p := g.attrsP.Load(); p != nil {
		return *p
	}
	return nil
}

func (g *gwSession) setAttrs(m map[string]string) {
	if m == nil {
		g.attrsP.Store(nil)
		return
	}
	g.attrsP.Store(&m)
}

// mergeAttrs applies a SetAttrs: an empty value deletes the key. The result is
// capped at gateway.MaxAttrsSize and is a fresh map (readers never see a
// partial update).
func (g *gwSession) mergeAttrs(upd map[string]string) error {
	cur := g.attrs()
	out := make(map[string]string, len(cur)+len(upd))
	for k, v := range cur {
		out[k] = v
	}
	for k, v := range upd {
		if v == "" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	size := 0
	for k, v := range out {
		size += len(k) + len(v)
	}
	if size > gateway.MaxAttrsSize {
		return gateway.ErrAttrsTooLarge
	}
	g.setAttrs(out)
	return nil
}

func newGwSession(ref gateway.SessionRef, version ocppj.Version, info *gateway.SessionInfo) *gwSession {
	g := &gwSession{ref: ref, version: version}
	g.infoP.Store(info)
	return g
}

func (g *gwSession) info() *gateway.SessionInfo { return g.infoP.Load() }

func (g *gwSession) setInfo(i *gateway.SessionInfo) { g.infoP.Store(i) }
