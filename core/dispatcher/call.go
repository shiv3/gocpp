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
	sendPayload := reqPayload
	if c.cfg.SchemaMode == SchemaModeLenient {
		out, herr := c.lenientValidate(action, "request", reqPayload)
		if herr != nil {
			return nil, herr
		}
		sendPayload = out
	} else if err := c.schemaValidationError(action, "request", reqPayload); err != nil {
		if c.cfg.SchemaMode == SchemaModeStrict {
			return nil, err
		}
	}
	raw, err := ocppj.EncodeCall(msgID, action, sendPayload)
	if err != nil {
		return nil, fmt.Errorf("encode call: %w", err)
	}
	release, err := c.acquireOutboundSlot(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

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
		respPayload := res.payload
		if c.cfg.SchemaMode == SchemaModeLenient {
			out, herr := c.lenientValidate(action, "response", res.payload)
			if herr != nil {
				c.maybeSendResultError(msgID, herr)
				return nil, herr
			}
			respPayload = out
		} else if verr := c.schemaValidationError(action, "response", res.payload); verr != nil {
			if c.cfg.SchemaMode == SchemaModeStrict {
				c.maybeSendResultError(msgID, verr)
				return nil, verr
			}
		}
		return respPayload, nil
	case <-c.ctx.Done():
		return nil, context.Cause(c.ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// maybeSendResultError notifies the peer that the CALLRESULT it sent could not
// be processed, by emitting a CALLRESULTERROR (OCPP 2.1 only). On older versions
// there is no such message type, so the failure stays local.
func (c *Conn) maybeSendResultError(msgID string, cause error) {
	if c.version != ocppj.V21 {
		return
	}
	c.sendCallResultError(msgID, ocppj.WrapCallError(ocppj.ErrorCodeFormatViolation, cause, nil))
}

func (c *Conn) acquireOutboundSlot(ctx context.Context) (func(), error) {
	if !c.cfg.SerializeOutboundCalls {
		return func() {}, nil
	}
	waitCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(c.ctx, cancel)
	err := c.outSem.Acquire(waitCtx, 1)
	stop()
	cancel()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if c.ctx.Err() != nil {
			return nil, context.Cause(c.ctx)
		}
		return nil, err
	}
	return func() { c.outSem.Release(1) }, nil
}
