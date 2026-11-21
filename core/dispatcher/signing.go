package dispatcher

import (
	"context"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/ocppj/signing"
)

// DecodeSigned resolves the payload to dispatch for a (possibly signed)
// inbound frame. For an unsigned frame it returns the payload unchanged. For a
// signed frame it verifies (when a Verifier is configured) or unwraps per OCPP
// 2.1 Part 4 §7.2. A non-nil reject means the message must not be processed.
func (p *Pipeline) DecodeSigned(ctx context.Context, frame *ocppj.Frame) (payload []byte, reject *ocppj.CallError) {
	if !frame.Signed {
		return frame.Payload, nil
	}
	if p.cfg.Verifier != nil {
		inner, _, err := p.cfg.Verifier.VerifyPayload(frame.Payload, frame.Action, frame.Type)
		if err == nil {
			return inner, nil
		}
		if p.cfg.RequireSignatureVerification {
			p.cfg.Logger.WarnContext(ctx, "signed message verification failed (rejecting)",
				"cp_id", p.cpID, "action", frame.Action, "err", err)
			return nil, ocppj.NewCallError(ocppj.ErrorCodeSecurityError, "signature verification failed", nil)
		}
		p.cfg.Logger.WarnContext(ctx, "signed message verification failed (unwrapping)",
			"cp_id", p.cpID, "action", frame.Action, "err", err)
	}
	inner, err := signing.UnwrapPayload(frame.Payload)
	if err != nil {
		return nil, ocppj.NewCallError(ocppj.ErrorCodeSecurityError, "malformed signed message", nil)
	}
	return inner, nil
}
