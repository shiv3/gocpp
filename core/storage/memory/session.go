package memory

import (
	"context"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

// globalDirectory is the cluster-wide table behind both the legacy
// PutGlobal/LookupGlobal calls and storage.SessionRegistry: one record per
// charge point, a per-charge-point epoch counter that survives removal, and one
// lease per gateway. Registries created by one InProcessHub share a directory.
type globalDirectory struct {
	now func() time.Time

	mu     sync.Mutex
	rec    map[string]storage.Session
	epoch  map[string]uint64
	leases map[string]lease
}

type lease struct {
	info    storage.GatewayInfo
	expires time.Time
}

func newGlobalDirectory() *globalDirectory {
	return &globalDirectory{
		now:    time.Now,
		rec:    make(map[string]storage.Session),
		epoch:  make(map[string]uint64),
		leases: make(map[string]lease),
	}
}

// SessionRegistryOption configures NewSessionRegistry.
type SessionRegistryOption func(*globalDirectory)

// WithClock replaces the time source used for lease expiry (tests).
func WithClock(now func() time.Time) SessionRegistryOption {
	return func(d *globalDirectory) { d.now = now }
}

// NewSessionRegistry returns an in-memory storage.SessionRegistry. It is shared
// by every Server that is handed the same value, which makes it the registry for
// tests, development and several servers in one process; across processes use a
// shared-store implementation such as addons/registry-redis.
func NewSessionRegistry(opts ...SessionRegistryOption) storage.SessionRegistry {
	d := newGlobalDirectory()
	for _, o := range opts {
		o(d)
	}
	return &connectionRegistry{local: make(map[string]storage.LiveConn), global: d}
}

// Legacy PutGlobal-style access, expressed as claims so that it shares the epoch
// order with the session API.

func (d *globalDirectory) put(cpID, instanceID string) {
	d.mu.Lock()
	d.epoch[cpID]++
	d.rec[cpID] = storage.Session{CPID: cpID, GatewayID: instanceID, Epoch: d.epoch[cpID]}
	d.mu.Unlock()
}

func (d *globalDirectory) lookup(cpID string) (string, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	r, ok := d.rec[cpID]
	return r.GatewayID, ok
}

func (d *globalDirectory) delete(cpID string) {
	d.mu.Lock()
	delete(d.rec, cpID)
	d.mu.Unlock()
}

func (d *globalDirectory) deleteIf(cpID, instanceID string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cur, ok := d.rec[cpID]; !ok || cur.GatewayID != instanceID {
		return false
	}
	delete(d.rec, cpID)
	return true
}

func (d *globalDirectory) claim(_ context.Context, s storage.Session) (uint64, *storage.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	var prev *storage.Session
	if p, ok := d.rec[s.CPID]; ok {
		prev = &p
	}
	d.epoch[s.CPID]++
	s.Epoch = d.epoch[s.CPID]
	d.rec[s.CPID] = s
	return s.Epoch, prev, nil
}

func (d *globalDirectory) setAttrs(_ context.Context, cpID string, epoch uint64, attrs map[string]string) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	cur, ok := d.rec[cpID]
	if !ok || cur.Epoch != epoch {
		return false, nil
	}
	cur.Meta, _ = storage.MergeAttrs(cur.Meta, attrs)
	d.rec[cpID] = cur
	return true, nil
}

func (d *globalDirectory) release(_ context.Context, cpID string, epoch uint64) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cur, ok := d.rec[cpID]; !ok || cur.Epoch != epoch {
		return false, nil
	}
	delete(d.rec, cpID)
	return true, nil
}

func (d *globalDirectory) reassert(_ context.Context, s storage.Session) (bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if cur, ok := d.rec[s.CPID]; ok {
		if cur.Epoch > s.Epoch {
			return false, nil
		}
		if cur.Epoch == s.Epoch && (cur.GatewayID != s.GatewayID || cur.Boot != s.Boot) {
			return false, nil
		}
	}
	if d.epoch[s.CPID] < s.Epoch {
		d.epoch[s.CPID] = s.Epoch
	}
	d.rec[s.CPID] = s
	return true, nil
}

// live reports whether the record's gateway lease is current. d.mu is held.
func (d *globalDirectory) live(s storage.Session) bool {
	if s.Boot == "" {
		return true
	}
	l, ok := d.leases[s.GatewayID]
	return ok && l.info.Boot == s.Boot && d.now().Before(l.expires)
}

func (d *globalDirectory) lookupSession(_ context.Context, cpID string) (storage.Session, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	s, ok := d.rec[cpID]
	if !ok || !d.live(s) {
		return storage.Session{}, false, nil
	}
	return s, true, nil
}

func (d *globalDirectory) lookupMany(_ context.Context, cpIDs []string) ([]storage.Session, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([]storage.Session, 0, len(cpIDs))
	for _, id := range cpIDs {
		if s, ok := d.rec[id]; ok && d.live(s) {
			out = append(out, s)
		}
	}
	return out, nil
}

func (d *globalDirectory) heartbeat(_ context.Context, g storage.GatewayInfo, ttl time.Duration) error {
	d.mu.Lock()
	d.leases[g.ID] = lease{info: g, expires: d.now().Add(ttl)}
	d.mu.Unlock()
	return nil
}

func (d *globalDirectory) gateways(_ context.Context) ([]storage.GatewayInfo, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	var out []storage.GatewayInfo
	for id, l := range d.leases {
		if now.Before(l.expires) {
			out = append(out, l.info)
		} else {
			delete(d.leases, id)
		}
	}
	return out, nil
}
