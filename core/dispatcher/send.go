package dispatcher

import (
	"context"
	"fmt"
	"time"

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

	sendPayload := reqPayload
	if c.cfg.SchemaMode == SchemaModeLenient {
		out, herr := c.lenientValidate(action, "request", reqPayload)
		if herr != nil {
			return herr
		}
		sendPayload = out
	} else if verr := c.schemaValidationError(action, "request", reqPayload); verr != nil {
		if c.cfg.SchemaMode == SchemaModeStrict {
			return verr
		}
	}
	raw, encErr := ocppj.EncodeSend(ocppj.NewMsgID(), action, sendPayload)
	if encErr != nil {
		return fmt.Errorf("encode send: %w", encErr)
	}
	if sErr := c.send(ctx, raw); sErr != nil {
		return fmt.Errorf("send: %w", sErr)
	}
	return nil
}

func (c *Conn) sendCallResultError(msgID string, ce *ocppj.CallError) {
	var details []byte
	if ce.Details != nil {
		details = mustJSON(ce.Details)
	}
	raw, err := ocppj.EncodeCallResultError(msgID, ce.WireCode(c.version), ce.Description, details)
	if err != nil {
		c.cfg.Logger.ErrorContext(c.ctx, "encode call result error", "err", err)
		return
	}
	_ = c.send(c.ctx, raw)
}
