package ocppj

import (
	"encoding/binary"
	"math/rand/v2"

	"github.com/oklog/ulid/v2"
)

// NewMsgID returns a unique, lexicographically time-sortable message ID: a ULID
// whose 80 random bits are not a secret. The bits come from math/rand/v2, whose
// global source is a per-thread ChaCha8 generator: no lock, so concurrent
// connections generating ids never contend (the previous crypto/rand + monotonic
// entropy sat behind one process-wide mutex on every outbound CALL). Ids created
// within the same millisecond are unique but not ordered among themselves; OCPP
// only needs uniqueness.
func NewMsgID() string {
	var id ulid.ULID
	_ = id.SetTime(ulid.Now())
	var e [10]byte
	binary.LittleEndian.PutUint64(e[:8], rand.Uint64())
	binary.LittleEndian.PutUint16(e[8:], uint16(rand.Uint32()))
	_ = id.SetEntropy(e[:])
	return id.String()
}
