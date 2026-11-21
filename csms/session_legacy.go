package csms

import (
	"context"
	"time"

	"github.com/shiv3/gocpp/core/storage"
)

// legacyRegistry presents the global half of a storage.ConnectionRegistry
// (PutGlobal / LookupGlobal / DeleteGlobal) as a SessionRegistry, so a registry
// written before SessionRegistry existed keeps working with WithInstanceID.
// The guarantees are those documented for the old API and no better: epochs are
// wall-clock nanoseconds (not registry-assigned), there is no atomic claim, and
// there are no gateway leases. A registry that implements SessionRegistry itself
// is used directly and gets the real guarantees.
type legacyRegistry struct {
	reg        storage.ConnectionRegistry
	instanceID string
}

func (l legacyRegistry) Claim(ctx context.Context, s storage.Session) (uint64, *storage.Session, error) {
	var prev *storage.Session
	if owner, ok, err := l.reg.LookupGlobal(ctx, s.CPID); err == nil && ok {
		prev = &storage.Session{CPID: s.CPID, GatewayID: owner}
	}
	if err := l.reg.PutGlobal(ctx, s.CPID, l.instanceID); err != nil {
		return 0, nil, err
	}
	return uint64(time.Now().UnixNano()), prev, nil
}

// Release is the old retractGlobal: an atomic compare-and-delete when the
// registry offers one, else a best-effort lookup then delete.
func (l legacyRegistry) Release(ctx context.Context, cpID string, _ uint64) (bool, error) {
	if d, ok := l.reg.(storage.OwnedGlobalDeleter); ok {
		return d.DeleteGlobalIf(ctx, cpID, l.instanceID)
	}
	owner, found, err := l.reg.LookupGlobal(ctx, cpID)
	if err != nil || !found || owner != l.instanceID {
		return false, err
	}
	return true, l.reg.DeleteGlobal(ctx, cpID)
}

func (l legacyRegistry) Reassert(ctx context.Context, s storage.Session) (bool, error) {
	return true, l.reg.PutGlobal(ctx, s.CPID, l.instanceID)
}

func (l legacyRegistry) Lookup(ctx context.Context, cpID string) (storage.Session, bool, error) {
	owner, ok, err := l.reg.LookupGlobal(ctx, cpID)
	if err != nil || !ok {
		return storage.Session{}, false, err
	}
	return storage.Session{CPID: cpID, GatewayID: owner}, true, nil
}

func (l legacyRegistry) LookupMany(ctx context.Context, cpIDs []string) ([]storage.Session, error) {
	var out []storage.Session
	for _, id := range cpIDs {
		if s, ok, err := l.Lookup(ctx, id); err != nil {
			return nil, err
		} else if ok {
			out = append(out, s)
		}
	}
	return out, nil
}

func (legacyRegistry) Heartbeat(context.Context, storage.GatewayInfo, time.Duration) error {
	return nil
}

func (legacyRegistry) Gateways(context.Context) ([]storage.GatewayInfo, error) { return nil, nil }
