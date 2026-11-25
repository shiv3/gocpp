package dispatcher

import (
	"context"
	"errors"
	"fmt"

	"github.com/shiv3/gocpp/core/ocppj"
	"go.opentelemetry.io/otel/attribute"
)

// Pipeline is the payload policy of a connection: signed-message unwrap,
// schema validation (strict / lenient / tolerant), lenient coercion, outbound
// validation and transform, and the mapping of handler failures to CALLERRORs.
// It needs only a Config, the negotiated version and the charge point id, not a
// *Conn, so a processing tier that receives (action, payload) from somewhere
// other than a WebSocket (a gateway link) applies exactly the same rules and
// produces the same error codes as an embedded connection.
//
// A Pipeline is a small value; copy it freely. The Config is shared and must
// not be modified after the pipeline is created.
//
// Experimental: this API may change before v1.0.
type Pipeline struct {
	cfg     *Config
	version ocppj.Version
	cpID    string
}

// NewPipeline returns the pipeline for a connection with the given negotiated
// version and charge point id (used in log lines only).
func NewPipeline(cfg *Config, version ocppj.Version, cpID string) Pipeline {
	return Pipeline{cfg: cfg, version: version, cpID: cpID}
}

// Version returns the OCPP version the pipeline applies.
func (p *Pipeline) Version() ocppj.Version { return p.version }

// InboundOutcome is the result of RunCall: what to send back for an inbound
// CALL, without having sent it.
//
// Experimental: this API may change before v1.0.
type InboundOutcome struct {
	// Status is the metrics status label: ok, panic, signature_invalid,
	// not_implemented, schema_invalid, timeout or error.
	Status string
	// Payload is the CALLRESULT payload; meaningful when Err is nil.
	Payload []byte
	// Err is the CALLERROR to send, or nil for a CALLRESULT.
	Err *ocppj.CallError
	// Wire is the error code to put on the wire for Err (after the
	// ErrorCodeMapper, if any).
	Wire string
	// NoReply is set when the ErrorCodeMapper suppressed the CALLERROR.
	NoReply bool
	// PeerFault is set when the CALL was rejected for its own content: it failed
	// schema validation or its payload could not be decoded. It is not set for an
	// unknown action, a handler failure or a locally produced invalid response.
	PeerFault bool

	faultReason string
}

// RunCall runs one inbound CALL through the pipeline: signature unwrap, the
// not-implemented check (h == nil), request validation, the handler under a
// tracing span, response validation and transform. It never sends anything and
// never panics: a handler panic becomes an InternalError outcome.
//
// ctx is the handler context (it carries the handler deadline, if any); h is
// the handler for frame.Action or nil when there is none.
func (p *Pipeline) RunCall(ctx context.Context, frame *ocppj.Frame, h func(ctx context.Context, payload []byte) ([]byte, error)) (out InboundOutcome) {
	out.Status = "ok"
	if p.cfg.ValidateErrorCodes {
		defer func() {
			if out.Err != nil && !out.NoReply && !ocppj.ValidErrorCode(p.version, out.Wire) {
				p.cfg.Logger.WarnContext(ctx, "handler CALLERROR code is not defined by the OCPP version; sending GenericError",
					"cp_id", p.cpID, "action", frame.Action, "code", out.Wire)
				out.Err = ocppj.NewCallError(ocppj.ErrorCodeGenericError, out.Err.Description+" (original code "+out.Wire+")", out.Err.Details)
				out.Wire = string(ocppj.ErrorCodeGenericError)
			}
		}()
	}
	// A panic in a user handler must not crash the whole peer/process: recover,
	// log, and answer with an InternalError CallError so the connection survives.
	defer func() {
		if r := recover(); r != nil {
			p.cfg.Logger.ErrorContext(ctx, "handler panic recovered",
				"cp_id", p.cpID, "action", frame.Action, "panic", r)
			p.fail(&out, "panic", ocppj.NewCallError(ocppj.ErrorCodeInternalError, "internal error", nil))
		}
	}()
	decoded, reject := p.DecodeSigned(ctx, frame)
	if reject != nil {
		p.fail(&out, "signature_invalid", reject)
		return out
	}

	if h == nil {
		p.builtin(&out, "not_implemented", ErrorSourceUnknownAction, frame.Action, ocppj.ErrUnknownAction, ocppj.NewCallError(
			ocppj.ErrorCodeNotImplemented, "action "+frame.Action+" not implemented", nil))
		return out
	}
	reqPayload, herr := p.ValidateInbound(ctx, frame.Action, "request", decoded)
	if herr != nil {
		p.builtin(&out, "schema_invalid", ErrorSourceSchema, frame.Action, herr, ocppj.NewCallError(SchemaErrorCode(herr), herr.Error(), nil))
		return out
	}

	// HandlerTimeout bounds the handler itself: it starts here, after the
	// signature and schema checks, as it always did.
	ctx, cancel := p.handlerTimeout(ctx)
	defer cancel()
	hctx, span := p.cfg.Tracer.Start(ctx, "ocpp.handler")
	if span.IsRecording() {
		span.SetAttributes(
			attribute.String("ocpp.action", frame.Action),
			attribute.String("ocpp.cp_id", p.cpID),
			attribute.String("ocpp.direction", "inbound"),
		)
	}
	resp, err := h(hctx, reqPayload)
	span.End()
	if err != nil {
		p.handlerFailed(ctx, hctx, frame.Action, err, &out)
		return out
	}
	respPayload, verr := p.ValidateOutbound(ctx, frame.Action, "response", resp)
	if verr != nil {
		// The local handler produced an invalid response, so no valid
		// CallResult can be sent. Return InternalError to the peer.
		p.fail(&out, "schema_invalid", ocppj.NewCallError(ocppj.ErrorCodeInternalError, verr.Error(), nil))
		return out
	}
	respPayload, terr := p.TransformOutbound(frame.Action, "response", respPayload)
	if terr != nil {
		p.fail(&out, "error", ocppj.NewCallError(ocppj.ErrorCodeInternalError, terr.Error(), nil))
		return out
	}
	out.Payload = respPayload
	return out
}

// handlerTimeout applies Config.HandlerTimeout, if set, to ctx.
func (p *Pipeline) handlerTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	if p.cfg.HandlerTimeout > 0 {
		return context.WithTimeout(ctx, p.cfg.HandlerTimeout)
	}
	return ctx, func() {}
}

// handlerFailed turns a handler error into an outcome.
func (p *Pipeline) handlerFailed(ctx, hctx context.Context, action string, err error, out *InboundOutcome) {
	if hctx.Err() != nil && errors.Is(err, context.DeadlineExceeded) {
		p.cfg.Logger.WarnContext(ctx, "handler exceeded its timeout",
			"cp_id", p.cpID, "action", action, "timeout", p.cfg.HandlerTimeout)
		p.fail(out, "timeout", ocppj.NewCallError(ocppj.ErrorCodeInternalError, "handler timed out", nil))
		return
	}
	var raw *RawCallError
	if errors.As(err, &raw) {
		*out = InboundOutcome{Status: "error", Err: raw.Err, Wire: raw.Wire, NoReply: raw.NoReply}
		return
	}
	ce := mapHandlerError(err)
	var de *decodeError
	if errors.As(err, &de) {
		p.builtin(out, "error", ErrorSourceDecode, action, de.err, ce)
		return
	}
	p.fail(out, "error", ce)
}

// fail sets out to answer with ce as is (no ErrorCodeMapper).
func (p *Pipeline) fail(out *InboundOutcome, status string, ce *ocppj.CallError) {
	*out = InboundOutcome{Status: status, Err: ce, Wire: ce.WireCode(p.version)}
}

// builtin sets out for a CALLERROR the dispatcher generated itself, which goes
// through the ErrorCodeMapper.
func (p *Pipeline) builtin(out *InboundOutcome, status string, src ErrorSource, action string, err error, def *ocppj.CallError) {
	wire, reply := p.MapBuiltinError(src, action, err, def, true)
	*out = InboundOutcome{Status: status, Err: def, Wire: wire, NoReply: !reply}
	switch src {
	case ErrorSourceSchema:
		out.PeerFault, out.faultReason = true, "schema"
	case ErrorSourceDecode:
		out.PeerFault, out.faultReason = true, "decode"
	}
}

// RawCallError is a handler error that is already a finished CALLERROR: the
// wire code is used as is (no ErrorCodeMapper, no re-spelling for the version)
// and NoReply suppresses the reply. A gateway's forwarder returns it for an
// error the processing tier decided.
//
// Experimental: this API may change before v1.0.
type RawCallError struct {
	Err     *ocppj.CallError
	Wire    string
	NoReply bool
}

func (e *RawCallError) Error() string {
	if e.Err == nil {
		return "raw call error"
	}
	return e.Err.Error()
}

// MapBuiltinError runs the configured ErrorCodeMapper for a CALLERROR the
// dispatcher generated itself. def is the default error; it returns the wire
// code to send and whether to reply.
func (p *Pipeline) MapBuiltinError(src ErrorSource, action string, err error, def *ocppj.CallError, defReply bool) (string, bool) {
	wire := def.WireCode(p.version)
	if p.cfg.ErrorCodeMapper == nil {
		return wire, defReply
	}
	return p.cfg.ErrorCodeMapper(ErrorContext{
		Source: src, Version: p.version, Action: action, Err: err, WireCode: wire, Reply: defReply,
	})
}

// ValidateInbound applies the configured schema policy to a payload received
// from the peer (kind "request" for an inbound CALL/SEND, "response" for the
// answer to our CALL). A non-nil error means the message must be rejected: a
// hard lenient violation, or any violation in strict mode. Otherwise out is the
// payload to continue with, possibly enum-normalised by lenient mode.
func (p *Pipeline) ValidateInbound(ctx context.Context, action, kind string, payload []byte) ([]byte, error) {
	if p.cfg.SchemaMode == SchemaModeLenient {
		return p.lenientValidate(ctx, action, kind, payload)
	}
	if err := p.schemaValidationError(ctx, action, kind, payload); err != nil && p.cfg.SchemaMode == SchemaModeStrict {
		return nil, err
	}
	return payload, nil
}

// ValidateOutbound applies the configured schema policy to a payload this side
// produced (kind "request" for outbound CALL/SEND, "response" for a handler
// result). It returns the payload to send, or the error that stops the send:
// a hard lenient violation, or any violation in strict mode.
func (p *Pipeline) ValidateOutbound(ctx context.Context, action, kind string, payload []byte) ([]byte, error) {
	if p.cfg.SkipOutboundValidation {
		return payload, nil
	}
	return p.ValidateInbound(ctx, action, kind, payload)
}

// TransformOutbound applies Config.OutboundTransform (date-time encoding) to an
// outbound payload. A failure is returned for a request and, for a response,
// handled by the caller.
func (p *Pipeline) TransformOutbound(action, kind string, payload []byte) ([]byte, error) {
	if p.cfg.OutboundTransform == nil {
		return payload, nil
	}
	out, err := p.cfg.OutboundTransform(p.version, action, kind, payload)
	if err != nil {
		return nil, fmt.Errorf("transform outbound %s: %w", kind, err)
	}
	return out, nil
}

func (p *Pipeline) recordSchemaValidationFailure(action, kind string) {
	if m, ok := p.cfg.Metrics.(schemaValidationMetricsHook); ok {
		m.SchemaValidationFailure(string(p.version), action, kind)
	}
}

// lenientValidate applies lenient validation. A non-nil hardErr means the
// caller must reject the message. Otherwise out is the payload to continue with,
// possibly enum-normalized.
func (p *Pipeline) lenientValidate(ctx context.Context, action, kind string, payload []byte) (out []byte, hardErr error) {
	if p.cfg.SchemaValidateLenient == nil {
		return payload, nil
	}
	out, soft, err := p.cfg.SchemaValidateLenient(p.version, action, kind, payload)
	if err != nil {
		p.recordSchemaValidationFailure(action, kind)
		return nil, err
	}
	for _, kw := range soft {
		if m, ok := p.cfg.Metrics.(schemaSoftViolationMetricsHook); ok {
			m.SchemaSoftViolation(string(p.version), action, kind, kw)
		}
	}
	if len(soft) > 0 {
		p.cfg.Logger.WarnContext(ctx, "schema soft violations passed (lenient)",
			"version", string(p.version), "action", action, "kind", kind, "keywords", soft)
	}
	return out, nil
}

func (p *Pipeline) schemaValidationError(ctx context.Context, action, kind string, payload []byte) error {
	if p.cfg.SchemaValidate == nil || p.cfg.SchemaMode == SchemaModeOff {
		return nil
	}
	err := p.cfg.SchemaValidate(p.version, action, kind, payload)
	if err == nil {
		return nil
	}
	p.recordSchemaValidationFailure(action, kind)
	if p.cfg.SchemaMode == SchemaModeTolerant {
		p.cfg.Logger.WarnContext(ctx, "schema validation failed",
			"version", string(p.version), "action", action, "kind", kind, "err", err)
	}
	return err
}

// RunSend is RunCall for an inbound SEND (OCPP 2.1): unconfirmed, so every
// failure is logged and dropped. h == nil means no handler: the frame is
// dropped.
func (p *Pipeline) RunSend(ctx context.Context, frame *ocppj.Frame, h func(ctx context.Context, payload []byte) error) {
	// A panic in a user SEND handler must not crash the peer/process. Unlike a
	// Call, a SEND has no reply, so recover, log, and drop.
	defer func() {
		if r := recover(); r != nil {
			p.cfg.Logger.ErrorContext(ctx, "SEND handler panic recovered",
				"cp_id", p.cpID, "action", frame.Action, "panic", r)
		}
	}()
	if h == nil {
		p.cfg.Logger.DebugContext(ctx, "no SEND handler registered, dropping",
			"cp_id", p.cpID, "action", frame.Action)
		return
	}
	decoded, reject := p.DecodeSigned(ctx, frame)
	if reject != nil {
		p.cfg.Logger.WarnContext(ctx, "dropping unverifiable signed SEND",
			"cp_id", p.cpID, "action", frame.Action, "code", reject.Code)
		return
	}
	reqPayload, err := p.ValidateInbound(ctx, frame.Action, "request", decoded)
	if err != nil {
		p.cfg.Logger.WarnContext(ctx, "dropping schema-invalid SEND",
			"cp_id", p.cpID, "action", frame.Action, "err", err)
		return
	}
	ctx, cancel := p.handlerTimeout(ctx)
	defer cancel()
	if err := h(ctx, reqPayload); err != nil {
		p.cfg.Logger.WarnContext(ctx, "SEND handler error",
			"cp_id", p.cpID, "action", frame.Action, "err", err)
	}
}
