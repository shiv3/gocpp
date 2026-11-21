package linkgrpc

import (
	"runtime"
	"sync"
	"time"
)

const (
	maxBatchFrames = 128     // frames per batch message
	maxBatchBytes  = 1 << 20 // encoded bytes per batch message (gRPC's default receive limit is 4 MiB)
	flushRounds    = 8       // rounds one sender flushes before handing over
	busyWindow     = 200 * time.Microsecond

	// MaxWriteLinger is the largest write linger accepted; larger values are
	// clamped. A stream busy enough to gain from a linger gathers its batches
	// within a few hundred microseconds; a longer wait only adds latency.
	MaxWriteLinger = 5 * time.Millisecond
)

// clampLinger maps a configured write linger to the value used.
func clampLinger(d time.Duration) time.Duration {
	return min(max(d, 0), MaxWriteLinger)
}

// sendQueue serialises writes to one stream and coalesces them: while a write
// is in progress, frames queued by other goroutines wait and then go out
// together (as one batch message when the peer supports it). Without a linger
// there is no timer: a frame that finds the queue idle is written at once by
// its own goroutine, so the low-rate latency is that of a plain Send.
//
// With linger > 0 (the write linger, off by default) a frame that finds the
// queue idle on a busy stream is not written at once: it waits up to linger for
// the frames about to follow and goes out with them. "Busy" means the smoothed
// interval between frames is at most half the linger, so that two or more
// further frames are expected within the wait; below that rate the queue
// behaves exactly as without a linger and no timer is ever armed. The wait ends
// early when a batch is full (frames or bytes).
//
// A frame accepted into a wait is in the same position as one queued behind a
// write in progress: push returns nil before it is written, and it is never
// reported as "not written" afterwards.
//
// Only the goroutine that finds the queue idle sees write errors; the others
// return nil because their frame was queued. That is safe because a broken
// stream ends the read loop, which fails every pending request.
type sendQueue[F any] struct {
	write func(F) error
	batch func([]F) error // nil: the peer has no batch support
	size  func(F) int     // encoded size of a frame, for the batch byte cap; nil: count frames only

	mu     sync.Mutex
	q      []F
	spare  []F
	active bool
	err    error
	last   time.Time // end of the previous write

	pushed  uint64 // frames accepted so far; a frame's sequence number is its position
	written uint64 // frames known to have been written (all with a lower sequence number)

	linger    time.Duration // longest wait for company on a busy stream; 0: never wait
	timer     *time.Timer   // ends a wait
	lingering bool          // active, and waiting for the timer rather than writing
	arrived   time.Time     // when the previous frame was pushed
	waitFrom  time.Time     // when the current wait began
	gap       time.Duration // smoothed interval between pushes
	qBytes    int           // encoded size of the frames queued during a wait
}

// gapWeight is the weight of one interval in the smoothed gap (1/gapWeight):
// the decision to wait reflects the last few dozen frames, so a handful of
// frames in quick succession on a quiet stream does not start a wait.
const gapWeight = 32

// busy records the arrival of a frame and reports whether the stream carries
// enough traffic for a wait to gather company: the smoothed interval between
// frames is at most half the linger. An interval counts as at most gapWeight
// lingers, so one long pause lifts the average above the threshold at once and
// the wait is off for the frames that follow it. Called with c.mu held and
// c.linger > 0.
func (c *sendQueue[F]) busy() bool {
	now := time.Now()
	if c.arrived.IsZero() {
		c.arrived, c.gap = now, c.linger
		return false
	}
	g := min(now.Sub(c.arrived), gapWeight*c.linger)
	c.arrived = now
	c.gap += (g - c.gap) / gapWeight
	return c.gap <= c.linger/2
}

// full reports whether the frames gathered during a wait fill a batch.
func (c *sendQueue[F]) full() bool {
	return len(c.q) >= maxBatchFrames || c.qBytes >= maxBatchBytes
}

// lingerEnd is the timer callback that ends a wait and writes what gathered.
func (c *sendQueue[F]) lingerEnd() {
	growStack()
	c.mu.Lock()
	if !c.lingering { // a push ended the wait first
		c.mu.Unlock()
		return
	}
	c.lingering = false
	_ = c.flush(false, 0) // c.mu held, released by flush
}

func (c *sendQueue[F]) push(f F) error {
	c.mu.Lock()
	if c.err != nil {
		err := c.err
		c.mu.Unlock()
		return err
	}
	c.q = append(c.q, f)
	seq := c.pushed
	c.pushed++
	wait := c.linger > 0 && c.busy()
	if c.lingering {
		if c.size != nil {
			c.qBytes += c.size(f)
		}
		if (c.full() || c.arrived.Sub(c.waitFrom) >= c.linger) && c.timer.Stop() {
			// The batch is full, or the wait is over and the timer is late (the
			// runtime fires sub-millisecond timers late while its threads are
			// idle): write now, on this goroutine. When Stop reports false the
			// timer has fired and lingerEnd writes.
			c.lingering = false
			return c.flush(true, seq) // c.mu held, released by flush
		}
		c.mu.Unlock()
		return nil
	}
	if c.active {
		c.mu.Unlock()
		return nil
	}
	c.active = true
	if wait {
		// Like a frame queued behind a write in progress, this one is accepted
		// and nil returned before it is written; a later write error breaks the
		// stream, which fails the request waiting for its answer.
		c.lingering = true
		c.waitFrom = c.arrived
		c.qBytes = 0
		if c.size != nil {
			c.qBytes = c.size(f)
		}
		if c.timer == nil {
			c.timer = time.AfterFunc(c.linger, c.lingerEnd)
		} else {
			c.timer.Reset(c.linger)
		}
		c.mu.Unlock()
		return nil
	}
	if !c.last.IsZero() && time.Since(c.last) < busyWindow {
		// The stream wrote a moment ago: other goroutines are likely about to
		// queue a frame. Let them run once before writing so the write carries
		// them too. Gosched costs nothing when no other goroutine is runnable,
		// and an idle stream (low rate) never takes this branch.
		c.mu.Unlock()
		runtime.Gosched()
		c.mu.Lock()
	}
	return c.flush(true, seq) // c.mu held, released by flush
}

// flush writes queued frames until the queue is empty. Called with c.mu held.
// The caller of push runs a bounded number of rounds, then a goroutine takes
// over so no request goroutine is stuck writing for others indefinitely.
//
// own is the sequence number of the owner's own frame. A write error is
// returned to the owner only when its own frame was not written: a failure
// while writing other goroutines' frames must not be reported as the owner's,
// or its caller would treat an already delivered request as not delivered and
// send it again. Those later failures are recorded in c.err instead; the
// goroutines that queued the frames see the broken stream.
func (c *sendQueue[F]) flush(owner bool, own uint64) error {
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
				_ = c.flush(false, 0)
			}()
			return nil
		}
		b := c.q
		c.q, c.spare = c.spare[:0], nil
		c.mu.Unlock()
		n, err := c.writeAll(b)
		clear(b)
		c.mu.Lock()
		c.last = time.Now()
		c.spare = b[:0]
		c.written += uint64(n)
		if err != nil {
			c.err = err
			c.q = nil
			c.active = false
			ownLost := owner && own >= c.written
			c.mu.Unlock()
			if ownLost {
				return err
			}
			return nil
		}
	}
}

// writeAll writes b and returns how many leading frames are known to have been
// written. A failed batch counts as none of its frames: the peer may or may not
// have received them.
func (c *sendQueue[F]) writeAll(b []F) (int, error) {
	done := 0
	if len(b) == 1 || c.batch == nil {
		for _, f := range b {
			if err := c.write(f); err != nil {
				return done, err
			}
			done++
		}
		return done, nil
	}
	for len(b) > 0 {
		n := c.batchLen(b)
		var err error
		if n == 1 {
			err = c.write(b[0])
		} else {
			err = c.batch(b[:n])
		}
		if err != nil {
			return done, err
		}
		done += n
		b = b[n:]
	}
	return done, nil
}

// handlerStack is the frame size of growStack: large enough that the runtime,
// which doubles a stack until the frame fits, goes from a fresh goroutine's
// 2 KB straight to 16 KB.
const handlerStack = 8 << 10

// growStack grows the calling goroutine's stack once, up front. Decoding and
// handling a message (or encoding and writing a batch) needs several KB; a
// goroutine that starts with 2 KB grows by copying, up to three times, each
// time with more frames to walk (12% of a backend's CPU at 35k msg/s). The
// large frame here makes the runtime grow once, while the stack is nearly
// empty and the copy nearly free. The frame is gone when growStack returns;
// the stack stays grown, all of it free for what the caller calls next.
//
//go:noinline
func growStack() {
	var frame [handlerStack]byte
	touchFrame(&frame)
}

//go:noinline
func touchFrame(*[handlerStack]byte) {}

// batchLen is how many leading frames of b go into the next batch: at most
// maxBatchFrames and, when sizes are known, at most maxBatchBytes (always at
// least one, so a single large frame is sent alone rather than stalling).
func (c *sendQueue[F]) batchLen(b []F) int {
	n := min(len(b), maxBatchFrames)
	if c.size == nil {
		return n
	}
	total := 0
	for i := 0; i < n; i++ {
		total += c.size(b[i])
		if total > maxBatchBytes {
			return max(i, 1)
		}
	}
	return n
}
