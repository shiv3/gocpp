package linkgrpc

import (
	"runtime"
	"sync"
	"time"
)

const (
	maxBatchFrames = 128 // frames per batch message
	flushRounds    = 8   // rounds one sender flushes before handing over
	busyWindow     = 200 * time.Microsecond
)

// sendQueue serialises writes to one stream and coalesces them: while a write
// is in progress, frames queued by other goroutines wait and then go out
// together (as one batch message when the peer supports it). There is no timer:
// a frame that finds the queue idle is written at once by its own goroutine, so
// the low-rate latency is that of a plain Send.
//
// Only the goroutine that finds the queue idle sees write errors; the others
// return nil because their frame was queued. That is safe because a broken
// stream ends the read loop, which fails every pending request.
type sendQueue[F any] struct {
	write func(F) error
	batch func([]F) error // nil: the peer has no batch support

	mu     sync.Mutex
	q      []F
	spare  []F
	active bool
	err    error
	last   time.Time // end of the previous write
}

func (c *sendQueue[F]) push(f F) error {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.q = append(c.q, f)
	if c.active {
		c.mu.Unlock()
		return nil
	}
	c.active = true
	if !c.last.IsZero() && time.Since(c.last) < busyWindow {
		// The stream wrote a moment ago: other goroutines are likely about to
		// queue a frame. Let them run once before writing so the write carries
		// them too. Gosched costs nothing when no other goroutine is runnable,
		// and an idle stream (low rate) never takes this branch.
		c.mu.Unlock()
		runtime.Gosched()
		c.mu.Lock()
	}
	return c.flush(true) // c.mu held, released by flush
}

// flush writes queued frames until the queue is empty. Called with c.mu held.
// The caller of push runs a bounded number of rounds, then a goroutine takes
// over so no request goroutine is stuck writing for others indefinitely.
func (c *sendQueue[F]) flush(owner bool) error {
	for round := 0; ; round++ {
		if len(c.q) == 0 {
			c.active = false
			c.mu.Unlock()
			return nil
		}
		if owner && round >= flushRounds {
			c.mu.Unlock()
			go func() {
				c.mu.Lock()
				_ = c.flush(false)
			}()
			return nil
		}
		b := c.q
		c.q, c.spare = c.spare[:0], nil
		c.mu.Unlock()
		err := c.writeAll(b)
		clear(b)
		c.mu.Lock()
		c.last = time.Now()
		c.spare = b[:0]
		if err != nil {
			c.err = err
			c.q = nil
			c.active = false
			c.mu.Unlock()
			return err
		}
	}
}

func (c *sendQueue[F]) writeAll(b []F) error {
	if len(b) == 1 || c.batch == nil {
		for _, f := range b {
			if err := c.write(f); err != nil {
				return err
			}
		}
		return nil
	}
	for len(b) > 0 {
		n := min(len(b), maxBatchFrames)
		var err error
		if n == 1 {
			err = c.write(b[0])
		} else {
			err = c.batch(b[:n])
		}
		if err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// handlerStack is the stack space a request goroutine reserves up front.
// Decoding and handling a message needs several KB; a goroutine that starts
// with 2 KB grows by copying, three times, per request (12% of a backend's CPU
// at 35k msg/s). Reserving the frame makes the runtime grow once, now, while
// the stack is empty and the copy is nearly free.
const handlerStack = 6 << 10

//go:noinline
func reserveStack(*[handlerStack]byte) {}
