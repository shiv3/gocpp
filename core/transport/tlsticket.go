package transport

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/binary"
	"sync"
	"time"
)

// DefaultTicketRotation is the rotation period used by RotateSharedTicketKeys
// when none is given.
const DefaultTicketRotation = 12 * time.Hour

// SharedTicketKeys derives the TLS session ticket keys for the rotation window
// containing now from a secret shared by all gateways. The first key encrypts new
// tickets; the previous and next windows' keys are also accepted, which absorbs
// clock skew between pods and lets a ticket live across one rotation. Every
// process that knows the secret computes the same keys, so a charge point that
// reconnects to a different pod resumes its TLS session instead of paying a full
// handshake (which matters in a reconnect storm).
func SharedTicketKeys(secret []byte, now time.Time, rotate time.Duration) [][32]byte {
	if rotate <= 0 {
		rotate = DefaultTicketRotation
	}
	w := now.Unix() / int64(rotate.Seconds())
	key := func(window int64) [32]byte {
		m := hmac.New(sha256.New, secret)
		m.Write([]byte("gocpp tls session ticket key"))
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], uint64(window))
		m.Write(b[:])
		var k [32]byte
		copy(k[:], m.Sum(nil))
		return k
	}
	return [][32]byte{key(w), key(w - 1), key(w + 1)}
}

// RotateSharedTicketKeys installs SharedTicketKeys on cfg and renews them as the
// rotation window changes, until stop is called. The secret must be the same on
// every gateway and kept private (anyone holding it can decrypt resumed
// sessions); cfg must not be shared with another rotation.
func RotateSharedTicketKeys(cfg *tls.Config, secret []byte, rotate time.Duration) (stop func()) {
	if rotate <= 0 {
		rotate = DefaultTicketRotation
	}
	apply := func() { cfg.SetSessionTicketKeys(SharedTicketKeys(secret, time.Now(), rotate)) }
	apply()
	done := make(chan struct{})
	var once sync.Once
	go func() {
		// Check often relative to the window so a skewed clock converges quickly.
		t := time.NewTicker(max(rotate/60, time.Second))
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				apply()
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
}
