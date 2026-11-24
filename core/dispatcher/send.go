package dispatcher

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
)

// send writes a pre-framed payload on the calling goroutine and returns the
// write result. There is no writer goroutine: the sender takes the connection's
// write lock (FIFO, so frames of concurrent senders go out in lock order),
// writes, runs the frame observer while still holding the lock (observer order
// is wire order) and releases it.
//
// A sender that is still waiting for the lock leaves promptly when ctx or the
// connection ends; one that holds it is bounded by WriteTimeout (and by the
// transport closing on teardown). A failed or timed-out write cancels the
// connection.
func (c *Conn) send(ctx context.Context, payload []byte) error {
	if c.ctx.Err() != nil {
		return context.Cause(c.ctx)
	}
	c.sampleQueueDepth(int(c.wwait.Add(1)))
	defer c.wwait.Add(-1)
	select {
	case c.wlock <- struct{}{}:
	default:
		select {
		case c.wlock <- struct{}{}:
		case <-c.ctx.Done():
			return context.Cause(c.ctx)
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	defer func() { <-c.wlock }()
	// Nothing is written once teardown has begun (it takes the lock after this
	// check can no longer succeed), so no write or observer call follows done.
	if c.ctx.Err() != nil {
		return context.Cause(c.ctx)
	}
	// Stamped before the write and undone if it fails: a peer that reads the
	// frame immediately must already see LastSentAt set (stamping after the write
	// raced with it).
	now := time.Now().UnixNano()
	prevSent := c.lastSent.Swap(now)
	err := c.write(payload)
	if err != nil {
		c.lastSent.CompareAndSwap(now, prevSent)
	}
	c.observeOutbound(payload, err)
	if err != nil {
		c.cancel(fmt.Errorf("write: %w", err))
	}
	return err
}

// sendCallResult writes the CALLRESULT and reports whether it was written.
func (c *Conn) sendCallResult(msgID string, payload []byte) bool {
	raw, err := ocppj.EncodeCallResult(msgID, payload)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call result", "err", err)
		return false
	}
	return c.send(c.ctx, raw) == nil
}

// write performs one socket write under WriteTimeout. The write ctx is never
// cancelled with the connection (cancelling an in-flight write makes the
// websocket library drop the TCP connection without a close frame; teardown
// closes the socket properly instead).
//
// With a transport.Aborter the timeout is enforced by a lazily armed
// per-connection timer (see writeWatchdog) and the write runs on the
// non-cancellable noCancel ctx, which also lets the library skip its own
// per-write cancellation hook. Otherwise each write gets a WithTimeout ctx.
func (c *Conn) write(payload []byte) error {
	if c.aborter == nil {
		wctx, cancel := context.WithTimeout(c.noCancel, c.cfg.WriteTimeout)
		defer cancel()
		return c.ws.Write(wctx, payload)
	}
	// Store, then arm: the watchdog callback does the mirror image (clear, then
	// re-check), so a write that starts while the timer is winding down is never
	// left unwatched. Writes are serialized by wlock, so wStart has one writer.
	c.wStart.Store(int64(time.Since(c.monoBase)) + 1)
	if c.wArmed.CompareAndSwap(false, true) {
		c.armWriteTimer(c.cfg.WriteTimeout)
	}
	err := c.ws.Write(c.noCancel, payload)
	c.wStart.Store(0)
	return err
}

// armWriteTimer starts (first use) or resets the write watchdog timer. Only the
// holder of the wArmed false->true transition calls it.
func (c *Conn) armWriteTimer(d time.Duration) {
	if t := c.wTimer.Load(); t != nil {
		t.Reset(d)
		return
	}
	c.wTimer.Store(time.AfterFunc(d, c.writeWatchdog))
}

// writeWatchdog runs when the write timer fires. Between writes it disarms;
// while a write is in flight it either enforces WriteTimeout (cancel the
// connection with a write cause, then abort the transport so the blocked write
// returns) or re-arms for the time that is left, so the timeout is exact for a
// write that started late in an armed period.
func (c *Conn) writeWatchdog() {
	if !c.enterBG() {
		return
	}
	defer c.leaveBG()
	if c.ctx.Err() != nil {
		return
	}
	wt := c.cfg.WriteTimeout
	for {
		s := c.wStart.Load()
		if s == 0 {
			c.wArmed.Store(false)
			if c.wStart.Load() == 0 || !c.wArmed.CompareAndSwap(false, true) {
				return // idle, or the writer re-armed it
			}
			continue // a write began meanwhile: watch it
		}
		elapsed := time.Since(c.monoBase) - time.Duration(s-1)
		if elapsed >= wt {
			c.cancel(fmt.Errorf("write: %w", context.DeadlineExceeded))
			_ = c.aborter.Abort()
			return
		}
		t := c.wTimer.Load()
		for t == nil { // the arming write has not stored the timer yet
			runtime.Gosched()
			t = c.wTimer.Load()
		}
		t.Reset(wt - elapsed)
		return
	}
}

// maxErrReplies bounds the protocol-error replies in flight per connection.
const maxErrReplies = 4

// trySendCallErrorWire answers a malformed frame without ever blocking the
// caller: the reader uses it, and blocking there would stall pong/close
// processing behind a slow peer. The reply is sent from a tracked background
// goroutine; at most maxErrReplies exist per connection and beyond that the
// reply is dropped, so a flood of bad frames cannot fan out.
func (c *Conn) trySendCallErrorWire(msgID, wire, desc string) {
	raw, err := ocppj.EncodeCallError(msgID, wire, desc, nil)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call error", "err", err)
		return
	}
	c.queueErrorReply(msgID, raw)
}

func (c *Conn) queueErrorReply(msgID string, raw []byte) {
	if c.errReplies.Add(1) <= maxErrReplies && c.goBG(func() {
		defer c.errReplies.Add(-1)
		_ = c.send(c.ctx, raw)
	}) {
		return
	}
	c.errReplies.Add(-1)
	c.cfg.Logger.WarnContext(c.ctx, "too many pending error replies; dropping call error",
		"cp_id", c.id, "msg_id", msgID)
}

// SendCallError queues a CALLERROR frame [4, msgID, code, description, details]
// for the peer with exactly the given wire code, description and details (raw
// JSON, "{}" when empty). It does not wait for the write and never blocks the
// caller; the number of queued error replies per connection is bounded, beyond
// which the error is dropped with a warning. It is for embedders that answer a
// frame themselves (see Config.ParseErrorHook).
func (c *Conn) SendCallError(msgID, code, description string, details []byte) {
	raw, err := ocppj.EncodeCallError(msgID, code, description, details)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call error", "err", err)
		return
	}
	c.queueErrorReply(msgID, raw)
}

func (c *Conn) sendCallErrorWire(msgID, wire string, ce *ocppj.CallError) {
	details := callErrorDetails(ce)
	raw, err := ocppj.EncodeCallError(msgID, wire, ce.Description, details)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call error", "err", err)
		return
	}
	_ = c.send(c.ctx, raw)
}

// DoSend sends an OCPP 2.1 SEND (MessageTypeId 6): an unconfirmed message with
// no response. It returns once the frame has been written. SEND has no
// synchronicity constraint, so it does not acquire the outbound serialization
// slot used by request/response calls. SEND is OCPP 2.1 only.
func DoSend(ctx context.Context, c *Conn, action string, reqPayload []byte) (err error) {
	if c.version != ocppj.V21 {
		return fmt.Errorf("%w: SEND requires OCPP 2.1, connection is %s", ocppj.ErrUnsupportedVersion, c.version)
	}
	start := time.Now()
	c.cfg.Metrics.CallStarted(action, "outbound")
	defer func() {
		status := "ok"
		if err != nil {
			status = "error"
		}
		c.cfg.Metrics.CallCompleted(action, "outbound", time.Since(start), status)
	}()

	sendPayload, verr := c.pipe.ValidateOutbound(ctx, action, "request", reqPayload)
	if verr != nil {
		return verr
	}
	sendPayload, err = c.pipe.TransformOutbound(action, "request", sendPayload)
	if err != nil {
		return err
	}
	msgID := ocppj.OutboundMessageID(ctx)
	ocppj.NotifyMessageID(ctx, msgID)
	var (
		raw    []byte
		encErr error
	)
	if c.cfg.Signer != nil {
		signed, serr := c.cfg.Signer.SignPayload(action, ocppj.Send, sendPayload)
		if serr != nil {
			return fmt.Errorf("sign send: %w", serr)
		}
		raw, encErr = ocppj.EncodeSignedSend(msgID, action, signed)
	} else {
		raw, encErr = ocppj.EncodeSend(msgID, action, sendPayload)
	}
	if encErr != nil {
		return fmt.Errorf("encode send: %w", encErr)
	}
	if sErr := c.send(ctx, raw); sErr != nil {
		return fmt.Errorf("send: %w", sErr)
	}
	return nil
}

func (c *Conn) sendCallResultError(msgID string, ce *ocppj.CallError) {
	details := callErrorDetails(ce)
	raw, err := ocppj.EncodeCallResultError(msgID, ce.WireCode(c.version), ce.Description, details)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call result error", "err", err)
		return
	}
	_ = c.send(c.ctx, raw)
}

// callErrorDetails returns the errorDetails to put on the wire for ce: the raw
// JSON if set, else the encoded Details map, else nil (an empty object).
func callErrorDetails(ce *ocppj.CallError) []byte {
	switch {
	case len(ce.RawDetails) > 0:
		return ce.RawDetails
	case ce.Details != nil:
		return mustJSON(ce.Details)
	}
	return nil
}

// badFrameCloseReason is the close reason sent when MaxBadFrames is exceeded.
const badFrameCloseReason = "too many invalid frames"

// noteBadFrame counts one bad inbound frame (see Config.MaxBadFrames) and, once
// the limit is reached within the window, closes the connection with 1008.
func (c *Conn) noteBadFrame(reason string) {
	if m, ok := c.cfg.Metrics.(badFrameMetricsHook); ok {
		m.BadFrame(string(c.version), reason)
	}
	limit := c.cfg.MaxBadFrames
	if limit <= 0 {
		return
	}
	now := time.Now()
	c.badMu.Lock()
	if c.badCount == 0 || (c.cfg.BadFrameWindow > 0 && now.Sub(c.badStart) > c.cfg.BadFrameWindow) {
		c.badCount, c.badStart = 0, now
	}
	c.badCount++
	trip := c.badCount == limit
	c.badMu.Unlock()
	if !trip {
		return
	}
	c.cfg.Logger.WarnContext(c.ctx, "closing connection: too many invalid frames",
		"cp_id", c.id, "limit", limit, "last_reason", reason)
	c.AbortWithStatus(transport.StatusPolicyViolation, badFrameCloseReason)
}
