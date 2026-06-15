package dispatcher

import (
	"context"
	"fmt"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
)

// DoCall sends an OCPP Call and waits for the matching CallResult or CallError.
// It returns the raw response payload; typed encoding/decoding happens in the
// csms/cp generic wrappers.
func DoCall(ctx context.Context, c *Conn, action string, reqPayload []byte) (_ []byte, err error) {
	start := time.Now()
	c.cfg.Metrics.CallStarted(action, "outbound")
	defer func() {
		status := "ok"
		if err != nil {
			status = "error"
		}
		c.cfg.Metrics.CallCompleted(action, "outbound", time.Since(start), status)
	}()

	msgID := ocppj.NewMsgID()
	raw, err := ocppj.EncodeCall(msgID, action, reqPayload)
	if err != nil {
		return nil, fmt.Errorf("encode call: %w", err)
	}

	pc := &pendingCall{
		msgID:  msgID,
		action: action,
		respCh: make(chan rawResult, 1),
	}

	timeoutCtx, cancel := context.WithTimeout(ctx, c.cfg.CallTimeout)
	defer cancel()
	pc.cancel = context.AfterFunc(timeoutCtx, func() {
		c.pending.resolve(msgID, rawResult{err: ocppj.ErrCallTimeout})
	})

	c.pending.add(msgID, pc)
	c.cfg.Metrics.PendingDelta(1)
	defer func() {
		c.pending.remove(msgID)
		c.cfg.Metrics.PendingDelta(-1)
	}()

	if err := c.send(ctx, raw); err != nil {
		return nil, fmt.Errorf("send: %w", err)
	}

	select {
	case res := <-pc.respCh:
		if res.err != nil {
			return nil, res.err
		}
		return res.payload, nil
	case <-c.ctx.Done():
		return nil, context.Cause(c.ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
