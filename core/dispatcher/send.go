package dispatcher

import (
	"context"

	"github.com/shiv3/gocpp/core/ocppj"
)

// send enqueues a pre-framed payload to the writer and waits for the write result.
func (c *Conn) send(ctx context.Context, payload []byte) error {
	sent := make(chan error, 1)
	select {
	case c.out <- outbound{payload: payload, sentCh: sent}:
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case err := <-sent:
		return err
	case <-c.ctx.Done():
		return context.Cause(c.ctx)
	}
}

func (c *Conn) sendCallResult(msgID string, payload []byte) {
	raw, err := ocppj.EncodeCallResult(msgID, payload)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call result", "err", err)
		return
	}
	_ = c.send(c.ctx, raw)
}

func (c *Conn) sendCallError(msgID string, ce *ocppj.CallError) {
	var details []byte
	if ce.Details != nil {
		details = mustJSON(ce.Details)
	}
	raw, err := ocppj.EncodeCallError(msgID, ce.WireCode(c.version), ce.Description, details)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call error", "err", err)
		return
	}
	_ = c.send(c.ctx, raw)
}
