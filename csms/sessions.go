package csms

import (
	"context"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/oklog/ulid/v2"
	"github.com/shiv3/gocpp/core/storage"
)

// SessionConfig tunes how a Server uses a storage.SessionRegistry. Zero values
// select the defaults.
type SessionConfig struct {
	// GatewayID names this process in the registry. Default: WithInstanceID, else
	// a random id (fine for a registry that is not shared).
	GatewayID string
	// Version is this process's release version: lease label "version", copied
	// into every session as Session.GatewayVersion.
	Version string
	// Labels are extra lease labels (see storage.GatewayInfo.Labels), for example
	// the canary group. Change them at runtime with Server.SetGatewayLabel; the
	// label "policy_rev" is reserved for the routing policy revision a gateway has
	// applied.
	Labels map[string]string
	// Address is informational: where other processes can reach this one.
	Address string
	// LeaseTTL is how long the gateway lease lives without a heartbeat; sessions
	// of a process whose lease lapsed stop being routable (default 15s).
	LeaseTTL time.Duration
	// HeartbeatInterval is how often the lease is renewed (default 5s).
	HeartbeatInterval time.Duration
	// ReconcileInterval is the period of the full pass that compares local
	// sessions with the registry (default 30s).
	ReconcileInterval time.Duration
	// ClaimTimeout bounds the registry wait in the connect path; on expiry the
	// connection is accepted unclaimed and claimed in the background (default 2s).
	ClaimTimeout time.Duration
}

func (c SessionConfig) withDefaults() SessionConfig {
	if c.LeaseTTL <= 0 {
		c.LeaseTTL = 15 * time.Second
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 5 * time.Second
	}
	if c.ReconcileInterval <= 0 {
		c.ReconcileInterval = 30 * time.Second
	}
	if c.ClaimTimeout <= 0 {
		c.ClaimTimeout = 2 * time.Second
	}
	return c
}

// reconcileBatch is the number of sessions checked per LookupMany call.
const reconcileBatch = 500

// sessionHolder is a local socket as the session manager sees it.
type sessionHolder interface {
	holderID() string
	holderSession() storage.Session
	setHolderSession(storage.Session)
	// alive reports whether the socket is still up.
	alive() bool
	// displace closes the socket because a newer connection owns the id.
	displace()
}

// sessionManager owns this process's view of the SessionRegistry: it claims and
// releases sessions on the connect path, keeps the gateway lease alive and runs
// the reconcile loop that makes the registry converge with the local sockets.
type sessionManager struct {
	reg  storage.SessionRegistry
	log  *slog.Logger
	cfg  SessionConfig
	boot string
	// loops is false for the legacy adapter, which has no leases or epochs to
	// maintain.
	loops bool

	holders func() []sessionHolder

	lmu    sync.RWMutex
	labels map[string]string

	draining   atomic.Bool
	leaseReady chan struct{}
	readyOnce  sync.Once
	kick       chan struct{}
}

func newSessionManager(reg storage.SessionRegistry, cfg SessionConfig, log *slog.Logger, loops bool, holders func() []sessionHolder) *sessionManager {
	cfg = cfg.withDefaults()
	if cfg.GatewayID == "" {
		cfg.GatewayID = ulid.Make().String()
	}
	labels := make(map[string]string, len(cfg.Labels))
	for k, v := range cfg.Labels {
		labels[k] = v
	}
	return &sessionManager{
		labels: labels,
		reg:    reg, log: log, cfg: cfg, boot: ulid.Make().String(), loops: loops, holders: holders,
		leaseReady: make(chan struct{}), kick: make(chan struct{}, 1),
	}
}

func (m *sessionManager) labelsCopy() map[string]string {
	m.lmu.RLock()
	defer m.lmu.RUnlock()
	out := make(map[string]string, len(m.labels)+1)
	for k, v := range m.labels {
		out[k] = v
	}
	if m.cfg.Version != "" {
		out["version"] = m.cfg.Version
	}
	return out
}

// setLabel changes one lease label (an empty value removes it) and renews the
// lease so readers see it at once.
func (m *sessionManager) setLabel(ctx context.Context, k, v string) {
	m.lmu.Lock()
	if v == "" {
		delete(m.labels, k)
	} else {
		m.labels[k] = v
	}
	m.lmu.Unlock()
	if m.loops {
		m.heartbeat(ctx)
	}
}

func (m *sessionManager) info() storage.GatewayInfo {
	state := storage.GatewayServing
	if m.draining.Load() {
		state = storage.GatewayDraining
	}
	return storage.GatewayInfo{
		ID: m.cfg.GatewayID, Boot: m.boot, Address: m.cfg.Address, State: state,
		Labels:   m.labelsCopy(),
		Sessions: len(m.holders()),
	}
}

// heartbeat renews the lease once.
func (m *sessionManager) heartbeat(ctx context.Context) {
	hctx, cancel := context.WithTimeout(ctx, m.cfg.ClaimTimeout)
	defer cancel()
	if err := m.reg.Heartbeat(hctx, m.info(), m.cfg.LeaseTTL); err != nil {
		m.log.Error("csms: session registry heartbeat failed", "gateway", m.cfg.GatewayID, "error", err)
		return
	}
	m.readyOnce.Do(func() { close(m.leaseReady) })
}

// waitLease waits until the first heartbeat succeeded (a record without a lease
// is not routable) or the claim timeout passes.
func (m *sessionManager) waitLease(ctx context.Context) {
	if !m.loops {
		return
	}
	select {
	case <-m.leaseReady:
	case <-ctx.Done():
	case <-time.After(m.cfg.ClaimTimeout):
	}
}

// run keeps the lease and the registry in step with the local sessions until ctx
// ends.
func (m *sessionManager) run(ctx context.Context) {
	if !m.loops {
		return
	}
	m.heartbeat(ctx)
	hb := time.NewTicker(m.cfg.HeartbeatInterval)
	defer hb.Stop()
	rec := time.NewTicker(m.cfg.ReconcileInterval)
	defer rec.Stop()
	var retry <-chan time.Time
	for {
		select {
		case <-ctx.Done():
			return
		case <-hb.C:
			m.heartbeat(ctx)
		case <-rec.C:
			if m.reconcile(ctx, false) > 0 {
				retry = time.After(time.Second)
			}
		case <-m.kick:
			if retry == nil {
				retry = time.After(time.Second)
			}
		case <-retry:
			retry = nil
			if m.reconcile(ctx, true) > 0 {
				retry = time.After(time.Second)
			}
		}
	}
}

// claim registers a new session before its upgrade is accepted. When the
// registry does not answer in time the session is returned with epoch 0
// (unclaimed): the connection is served and the reconcile loop claims it later.
func (m *sessionManager) claim(ctx context.Context, s storage.Session) storage.Session {
	s.GatewayID, s.Boot, s.Epoch = m.cfg.GatewayID, m.boot, 0
	s.GatewayVersion = m.cfg.Version
	m.waitLease(ctx)
	cctx, cancel := context.WithTimeout(ctx, m.cfg.ClaimTimeout)
	defer cancel()
	epoch, _, err := m.reg.Claim(cctx, s)
	if err != nil {
		m.log.Error("csms: claim session failed; serving unclaimed",
			"cp_id", s.CPID, "gateway", m.cfg.GatewayID, "error", err)
		if m.loops {
			select {
			case m.kick <- struct{}{}:
			default:
			}
		}
		return s
	}
	s.Epoch = epoch
	return s
}

// release removes the session's record if it is still the current one.
func (m *sessionManager) release(ctx context.Context, s storage.Session) {
	if s.Epoch == 0 {
		return
	}
	if _, err := m.reg.Release(ctx, s.CPID, s.Epoch); err != nil {
		m.log.Error("csms: release session failed",
			"cp_id", s.CPID, "epoch", s.Epoch, "gateway", m.cfg.GatewayID, "error", err)
	}
}

// reconcile compares the local sessions with the registry and repairs the
// difference: a socket whose epoch is no longer current is closed, a missing
// record is reasserted (or re-claimed when a dead owner's newer record is in
// the way), an unclaimed socket is claimed, and current records are refreshed.
// With onlyUnclaimed it handles just the unclaimed sockets. It returns the
// number of sockets that still have no epoch.
func (m *sessionManager) reconcile(ctx context.Context, onlyUnclaimed bool) (unclaimed int) {
	hs := m.holders()
	if onlyUnclaimed {
		kept := hs[:0]
		for _, h := range hs {
			if h.holderSession().Epoch == 0 {
				kept = append(kept, h)
			}
		}
		hs = kept
	}
	for len(hs) > 0 && ctx.Err() == nil {
		n := min(len(hs), reconcileBatch)
		batch := hs[:n]
		hs = hs[n:]
		ids := make([]string, len(batch))
		for i, h := range batch {
			ids[i] = h.holderID()
		}
		lctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		recs, err := m.reg.LookupMany(lctx, ids)
		cancel()
		if err != nil {
			m.log.Error("csms: reconcile lookup failed", "gateway", m.cfg.GatewayID, "error", err)
			for _, h := range batch {
				if h.holderSession().Epoch == 0 {
					unclaimed++
				}
			}
			continue
		}
		byID := make(map[string]storage.Session, len(recs))
		for _, r := range recs {
			byID[r.CPID] = r
		}
		for _, h := range batch {
			if !m.reconcileOne(ctx, h, byID) {
				unclaimed++
			}
		}
	}
	return unclaimed
}

// reconcileOne reports false when the socket is left without an epoch.
func (m *sessionManager) reconcileOne(ctx context.Context, h sessionHolder, live map[string]storage.Session) bool {
	s := h.holderSession()
	rec, haveLive := live[s.CPID]
	switch {
	case s.Epoch == 0:
		return m.reclaim(ctx, h, s)
	case haveLive && (rec.Epoch > s.Epoch || (rec.Epoch == s.Epoch && (rec.GatewayID != s.GatewayID || rec.Boot != s.Boot))):
		m.displace(h, s, rec)
		return true
	}
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	ok, err := m.reg.Reassert(opCtx, s)
	if err != nil {
		m.log.Error("csms: reassert session failed", "cp_id", s.CPID, "epoch", s.Epoch, "error", err)
		return true
	}
	if ok {
		return true
	}
	// The registry holds something newer or conflicting. A live newer owner
	// supersedes this socket; a dead one does not.
	cur, found, err := m.reg.Lookup(opCtx, s.CPID)
	if err != nil {
		m.log.Error("csms: reconcile lookup failed", "cp_id", s.CPID, "error", err)
		return true
	}
	if found {
		m.displace(h, s, cur)
		return true
	}
	return m.reclaim(ctx, h, s)
}

func (m *sessionManager) displace(h sessionHolder, s, cur storage.Session) {
	m.log.Warn("csms: closing superseded connection",
		"cp_id", s.CPID, "epoch", s.Epoch, "current_epoch", cur.Epoch, "current_gateway", cur.GatewayID)
	h.displace()
}

// reclaim assigns a fresh epoch to a socket that has none or whose record was
// lost to a dead owner.
func (m *sessionManager) reclaim(ctx context.Context, h sessionHolder, s storage.Session) bool {
	opCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	epoch, _, err := m.reg.Claim(opCtx, s)
	if err != nil {
		m.log.Error("csms: claim session failed", "cp_id", s.CPID, "error", err)
		return false
	}
	s.Epoch = epoch
	h.setHolderSession(s)
	if !h.alive() {
		// The socket ended while we were claiming; its teardown may have run
		// before it could see the new epoch.
		m.release(ctx, s)
	}
	return true
}
