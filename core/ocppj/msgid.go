package ocppj

import (
	"crypto/rand"
	"sync"

	"github.com/oklog/ulid/v2"
)

var (
	entropyMu sync.Mutex
	entropy   = ulid.Monotonic(rand.Reader, 0)
)

// NewMsgID returns a unique, lexicographically sortable message ID.
func NewMsgID() string {
	entropyMu.Lock()
	defer entropyMu.Unlock()
	return ulid.MustNew(ulid.Now(), entropy).String()
}
