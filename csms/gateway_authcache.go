package csms

import (
	"context"
	"crypto/sha256"
	"fmt"
	"maps"
	"sync"
	"time"

	"github.com/shiv3/gocpp/gateway"
)

type authKey [sha256.Size]byte

// authKeyOf hashes everything a connect decision is documented to depend on.
func authKeyOf(req gateway.ConnectRequest) authKey {
	h := sha256.New()
	w := func(s string) {
		var n [4]byte
		n[0], n[1], n[2], n[3] = byte(len(s)>>24), byte(len(s)>>16), byte(len(s)>>8), byte(len(s))
		_, _ = h.Write(n[:])
		_, _ = h.Write([]byte(s))
	}
	w(req.CPID)
	w(req.Path)
	for _, v := range req.Header.Values("Authorization") {
		w(v)
	}
	w("|")
	for _, p := range req.Subprotocol {
		w(p)
	}
	w("|")
	if len(req.PeerCerts) > 0 {
		w(string(req.PeerCerts[0]))
	}
	var k authKey
	h.Sum(k[:0])
	return k
}

type authEntry struct {
	cpID string
	rep  gateway.ConnectReply
	at   time.Time
}

// authCache holds the positive connect decisions of a gateway.
type authCache struct {
	cfg AuthCacheConfig
	now func() time.Time

	mu sync.Mutex
	m  map[authKey]authEntry
}

func newAuthCache(cfg AuthCacheConfig) *authCache {
	if cfg.TTL <= 0 {
		cfg.TTL = 5 * time.Minute
	}
	if cfg.StaleIfError < 0 {
		cfg.StaleIfError = 0
	} else if cfg.StaleIfError == 0 {
		cfg.StaleIfError = time.Hour
	}
	if cfg.MaxEntries <= 0 {
		cfg.MaxEntries = 100000
	}
	return &authCache{cfg: cfg, now: time.Now, m: make(map[authKey]authEntry)}
}

// cloneReply copies the maps so a session cannot change the cached decision.
func cloneReply(r gateway.ConnectReply) gateway.ConnectReply {
	r.Meta, r.Attrs = maps.Clone(r.Meta), maps.Clone(r.Attrs)
	if r.Timeouts != nil {
		t := *r.Timeouts
		r.Timeouts = &t
	}
	return r
}

func (c *authCache) get(k authKey, within time.Duration) (gateway.ConnectReply, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[k]
	if !ok || c.now().Sub(e.at) >= within {
		return gateway.ConnectReply{}, false
	}
	return cloneReply(e.rep), true
}

// fresh returns a decision young enough to skip the backend.
func (c *authCache) fresh(k authKey) (gateway.ConnectReply, bool) { return c.get(k, c.cfg.TTL) }

// stale returns a decision to use when the backend cannot answer.
func (c *authCache) stale(k authKey) (gateway.ConnectReply, bool) {
	if c.cfg.StaleIfError <= 0 {
		return gateway.ConnectReply{}, false
	}
	return c.get(k, c.cfg.TTL+c.cfg.StaleIfError)
}

func (c *authCache) put(k authKey, cpID string, rep gateway.ConnectReply) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if _, ok := c.m[k]; !ok && len(c.m) >= c.cfg.MaxEntries {
		limit := c.cfg.TTL + c.cfg.StaleIfError
		for kk, e := range c.m {
			if now.Sub(e.at) >= limit {
				delete(c.m, kk)
			}
		}
		for kk := range c.m {
			if len(c.m) < c.cfg.MaxEntries {
				break
			}
			delete(c.m, kk)
		}
	}
	c.m[k] = authEntry{cpID: cpID, rep: cloneReply(rep), at: now}
}

func (c *authCache) drop(k authKey) {
	c.mu.Lock()
	delete(c.m, k)
	c.mu.Unlock()
}

func (c *authCache) invalidate(cpID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k, e := range c.m {
		if cpID == "" || e.cpID == cpID {
			delete(c.m, k)
		}
	}
}

// InvalidateAuthCache forgets the cached connect decisions of a charge point (all
// of them when cpID is ""), for after a credential change. A no-op on a server
// without WithGatewayAuthCache.
func (s *Server) InvalidateAuthCache(cpID string) {
	if s.edge != nil && s.edge.auth != nil {
		s.edge.auth.invalidate(cpID)
	}
}

// InvalidateGatewayAuthCache tells every gateway to forget its cached connect
// decisions for cpID (all of them when cpID is ""), for after a credential
// change: without it a revoked credential keeps admitting reconnects until the
// cache's TTL (and, while this processing tier is unreachable, StaleIfError)
// runs out. Call it on a server created with WithGatewayLink. The gateways are
// the ones holding a lease in the session registry (WithSessionRegistry); without
// a registry the request goes to the link's only gateway (the in-process link).
// It reports the first failure after trying every gateway; a gateway that is
// down misses the request and recovers when its entries expire.
func (s *Server) InvalidateGatewayAuthCache(ctx context.Context, cpID string) error {
	if s.backend == nil {
		return ErrNoGatewayLink
	}
	ids := []string{""}
	if reg := s.cfg.sessionReg; reg != nil {
		gws, err := reg.Gateways(ctx)
		if err != nil {
			return fmt.Errorf("csms: list gateways: %w", err)
		}
		ids = ids[:0]
		for _, g := range gws {
			ids = append(ids, g.ID)
		}
	}
	var first error
	for _, id := range ids {
		err := s.backend.link.Control(ctx, gateway.ControlRequest{
			Target: gateway.SessionRef{CPID: cpID, GatewayID: id}, Op: gateway.ControlInvalidateAuth,
		})
		if err != nil && first == nil {
			first = fmt.Errorf("csms: invalidate auth cache on gateway %q: %w", id, err)
		}
	}
	return first
}
