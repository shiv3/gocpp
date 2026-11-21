package dispatcher

import "github.com/shiv3/gocpp/core/ocppj"

// Inbound CALL/SEND scheduling. There is no dispatcher goroutine: the reader
// starts a handler goroutine itself when a slot is free and otherwise parks the
// frame in a small bounded queue that finishing handlers drain.
//
// Everything is guarded by inMu: inQ (allocated on first use, dropped when it
// empties), running (handler goroutines in flight, at most maxHandlers),
// gateOpen (false between HoldInbound and ReleaseInbound) and inSpace (closed to
// wake the reader that waits for room). No I/O and no other lock is taken under
// inMu.
//
// Invariants, true whenever inMu is free:
//   - len(inQ) <= InboundQueueSize;
//   - running <= maxHandlers;
//   - gateOpen && len(inQ) > 0 implies running == maxHandlers, so every queued
//     frame has a running loop that will pop it (no lost wake-up, work
//     conserving);
//   - frames start in arrival order: a frame bypasses the queue only when the
//     queue is empty.
//
// The reader waits only when the queue is full (or, with a zero-size queue,
// when all handler slots are busy). That is the back-pressure; below it
// CALLRESULTs and pongs keep being read and resolved.

// enqueueInbound hands a CALL/SEND to a handler. It returns false when the
// connection context is done and the reader must stop.
func (c *Conn) enqueueInbound(f ocppj.Frame) bool {
	c.inMu.Lock()
	for {
		if c.ctx.Err() != nil {
			c.inMu.Unlock()
			return false
		}
		if c.gateOpen && len(c.inQ) == 0 && c.running < c.maxHandlers {
			c.running++
			c.inMu.Unlock()
			c.startHandler(f)
			return true
		}
		if len(c.inQ) < c.cfg.InboundQueueSize {
			c.inQ = append(c.inQ, f)
			c.inMu.Unlock()
			return true
		}
		if c.inSpace == nil {
			c.inSpace = make(chan struct{})
		}
		ch := c.inSpace
		c.inMu.Unlock()
		select {
		case <-ch:
		case <-c.ctx.Done():
			return false
		}
		c.inMu.Lock()
	}
}

// handlerLoop runs f and then keeps popping queued frames until none is left or
// the connection ends. It owns one unit of running.
func (c *Conn) handlerLoop(f ocppj.Frame) {
	if c.cfg.DeepHandlerStack || c.cfg.SchemaValidate != nil || c.cfg.SchemaValidateLenient != nil {
		// With validation in the handler path the chain is deep (the validator,
		// the codec, the reply encoder): pre-growing straight to 16 KB saves the
		// repeated stack copies, and the pre-growth also makes the small reserve
		// in runHandler a no-op. Without validation the reserve alone is the
		// cheapest (a 16 KB stack costs more than it saves on the shallow path).
		growDeepStack()
	}
	for {
		c.runHandler(f)
		if d := c.inboundDone; d != nil {
			d(f)
		}
		c.inMu.Lock()
		if len(c.inQ) == 0 || c.ctx.Err() != nil {
			c.inQ = nil
			c.running--
			c.wakeReaderLocked()
			c.inMu.Unlock()
			return
		}
		f = c.popLocked()
		c.inMu.Unlock()
	}
}

// popLocked removes the oldest queued frame and wakes a waiting reader.
func (c *Conn) popLocked() ocppj.Frame {
	f := c.inQ[0]
	c.inQ[0] = ocppj.Frame{}
	c.inQ = c.inQ[1:]
	if len(c.inQ) == 0 {
		c.inQ = nil
	}
	c.wakeReaderLocked()
	return f
}

func (c *Conn) wakeReaderLocked() {
	if c.inSpace != nil {
		close(c.inSpace)
		c.inSpace = nil
	}
}

// dropInbound discards frames that never started (teardown). Running handlers
// finish on their own.
func (c *Conn) dropInbound() {
	c.inMu.Lock()
	c.inQ = nil
	c.wakeReaderLocked()
	c.inMu.Unlock()
}
