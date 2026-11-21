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
//
// With SerializeOutboundCalls (the default) the call first waits its turn for the
// connection's single outstanding-CALL slot; CallTimeout starts only once it
// owns the slot, and the wait is bounded by ctx (and ocppj.ErrQueueFull when too
// many calls already wait).
func DoCall(ctx context.Context, c *Conn, action string, reqPayload []byte) ([]byte, error) {
	return c.call(ctx, action, reqPayload, true)
}

// call implements DoCall. With acquire false the caller already owns the slot.
func (c *Conn) call(ctx context.Context, action string, reqPayload []byte, acquire bool) (_ []byte, err error) {
	if !c.beginCall() {
		return nil, ocppj.ErrGatewayDraining
	}
	defer c.endCall()
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
	ocppj.NotifyMessageID(ctx, msgID)
	sendPayload, verr := c.pipe.ValidateOutbound(ctx, action, "request", reqPayload)
	if verr != nil {
		return nil, verr
	}
	sendPayload, err = c.pipe.TransformOutbound(action, "request", sendPayload)
	if err != nil {
		return nil, err
	}
	var raw []byte
	if c.cfg.Signer != nil {
		signed, serr := c.cfg.Signer.SignPayload(action, ocppj.Call, sendPayload)
		if serr != nil {
			return nil, fmt.Errorf("sign call: %w", serr)
		}
		raw, err = ocppj.EncodeSignedCall(msgID, action, signed)
	} else {
		raw, err = ocppj.EncodeCall(msgID, action, sendPayload)
	}
	if err != nil {
		return nil, fmt.Errorf("encode call: %w", err)
	}
	if acquire && c.cfg.SerializeOutboundCalls {
		if err := c.acquireSlot(ctx); err != nil {
			return nil, err
		}
		defer c.slot.release()
	}

	pc := &pendingCall{
		msgID:  msgID,
		action: action,
		respCh: make(chan rawResult, 1),
	}

	// One runtime timer is the call timeout; the final select below already
	// watches the caller's ctx and the connection's.
	pc.cancel = time.AfterFunc(c.cfg.CallTimeout, func() {
		c.pending.resolve(msgID, rawResult{err: ocppj.ErrCallTimeout})
	}).Stop

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
		respPayload, herr := c.pipe.ValidateInbound(ctx, action, "response", res.payload)
		if herr != nil {
			c.maybeSendResultError(msgID, herr)
			return nil, herr
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
