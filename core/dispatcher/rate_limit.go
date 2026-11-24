package dispatcher

import (
	"errors"
	"math"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
)

// ErrInboundRateLimited is the Err of the ErrorContext an ErrorCodeMapper sees
// for ErrorSourceRateLimit.
var ErrInboundRateLimited = errors.New("ocpp: inbound rate limit exceeded")

const rateLimitCloseReason = "inbound rate limit exceeded"

// rateBucket is a token bucket. The zero value is full on first use.
type rateBucket struct {
	mu     sync.Mutex
	tokens float64
	last   time.Time
	init   bool
}

// allow takes one token, refilling at rate per second up to burst (< 1 means
// max(1, ceil(rate))).
func (b *rateBucket) allow(rate float64, burst int, now time.Time) bool {
	capacity := float64(burst)
	if burst < 1 {
		capacity = math.Max(1, math.Ceil(rate))
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.init {
		b.init, b.tokens, b.last = true, capacity, now
	} else if d := now.Sub(b.last); d > 0 {
		b.tokens = math.Min(capacity, b.tokens+d.Seconds()*rate)
		b.last = now
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// inboundRateLimited applies Config.InboundRateAction to a CALL or SEND over the
// limit.
func (c *Conn) inboundRateLimited(frame ocppj.Frame) {
	outcome := "rejected"
	if c.cfg.InboundRateAction == RateLimitClose {
		outcome = "closed"
	}
	if m, ok := c.cfg.Metrics.(rateLimitMetricsHook); ok {
		m.InboundRateLimited(string(c.version), outcome)
	}
	if c.cfg.InboundRateAction == RateLimitClose {
		c.cfg.Logger.WarnContext(c.ctx, "closing connection: inbound rate limit exceeded",
			"cp_id", c.id, "rate", c.cfg.InboundRate, "action", frame.Action)
		c.AbortWithStatus(transport.StatusPolicyViolation, rateLimitCloseReason)
		return
	}
	c.cfg.Logger.DebugContext(c.ctx, "inbound rate limit exceeded; message refused",
		"cp_id", c.id, "action", frame.Action)
	if frame.Type != ocppj.Call || frame.MsgID == "" {
		return // a SEND is unconfirmed: nothing to answer
	}
	def := ocppj.NewCallError(ocppj.ErrorCodeGenericError, "rate limit exceeded", nil)
	if wire, reply := c.pipe.MapBuiltinError(ErrorSourceRateLimit, frame.Action, ErrInboundRateLimited, def, true); reply {
		c.trySendCallErrorWire(frame.MsgID, wire, def.Description)
	}
}
