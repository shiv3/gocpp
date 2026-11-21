package storage

import (
	"context"
	"errors"
	"time"
)

// ErrPolicyRevision is returned by PutPolicy when rev is not greater than prevRev.
var ErrPolicyRevision = errors.New("storage: policy revision must be greater than the previous one")

// PolicyStore holds the routing policy of a gateway fleet (docs/design/gateway.md
// 5.1.2): one opaque document with a strictly increasing revision, written
// with compare-and-set and pushed to watchers, so a promotion or rollback
// reaches every gateway in well under a second and two operators cannot
// overwrite each other.
//
// The document is opaque to the store; the routepolicy package defines it.
//
// Experimental: this API may change before v1.0.
type PolicyStore interface {
	// PutPolicy stores data as revision rev if the stored revision equals
	// prevRev (0 when none is stored). ok is false, with a nil error, when
	// another writer got there first. rev must be greater than prevRev.
	PutPolicy(ctx context.Context, prevRev, rev uint64, data []byte) (ok bool, err error)
	// Policy returns the current revision and document; rev is 0 and data nil
	// when no policy has been stored.
	Policy(ctx context.Context) (rev uint64, data []byte, err error)
	// WatchPolicy calls fn with the current policy (if any) and then with every
	// newer revision until ctx ends. fn is never called concurrently with
	// itself. A watcher survives store outages: it resumes and reports the
	// newest revision it missed.
	WatchPolicy(ctx context.Context, fn func(rev uint64, data []byte)) error
}

// PolicyEntry is one stored revision.
type PolicyEntry struct {
	Rev  uint64
	Data []byte
	At   time.Time
}

// PolicyHistory is implemented by stores that keep the last revisions, which
// is what a rollback (gocppctl route rollback) is written from.
//
// Experimental: this API may change before v1.0.
type PolicyHistory interface {
	// PolicyHistory returns the stored revisions, newest first.
	PolicyHistory(ctx context.Context) ([]PolicyEntry, error)
}

// PolicyHistoryLimit is how many revisions a store keeps for PolicyHistory.
const PolicyHistoryLimit = 20
