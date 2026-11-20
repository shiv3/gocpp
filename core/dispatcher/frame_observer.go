package dispatcher

import (
	"errors"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
)

// observeInbound reports a raw inbound frame. parsed/perr are the result of
// ocppj.Parse; for an unparsable frame whatever the error carries is used.
func (c *Conn) observeInbound(raw []byte, parsed ocppj.Frame, perr error) {
	if c.cfg.FrameObserver == nil {
		return
	}
	ev := observability.FrameEvent{
		CPID: c.id, Version: c.version, Direction: observability.FrameIn,
		Raw: raw, Time: time.Now(),
	}
	if perr == nil {
		ev.Type, ev.MsgID, ev.Action = parsed.Type, parsed.MsgID, parsed.Action
	} else {
		var perrP *ocppj.ProtocolError
		var ute *ocppj.UnknownMessageTypeError
		switch {
		case errors.As(perr, &perrP):
			ev.Type, ev.MsgID = ocppj.MessageType(perrP.MessageType), perrP.MsgID
		case errors.As(perr, &ute):
			ev.Type, ev.MsgID = ocppj.MessageType(ute.MessageType), ute.MsgID
		}
	}
	c.callObserver(ev)
}

// observeOutbound reports a frame the sender just attempted to write.
func (c *Conn) observeOutbound(raw []byte, werr error) {
	if c.cfg.FrameObserver == nil {
		return
	}
	ev := observability.FrameEvent{
		CPID: c.id, Version: c.version, Direction: observability.FrameOut,
		Raw: raw, Time: time.Now(), Err: werr,
	}
	// Best effort: our own encoder produced this, so it normally parses.
	if f, err := ocppj.Parse(raw); err == nil {
		ev.Type, ev.MsgID, ev.Action = f.Type, f.MsgID, f.Action
	}
	c.callObserver(ev)
}

func (c *Conn) callObserver(ev observability.FrameEvent) {
	defer func() {
		if r := recover(); r != nil {
			c.cfg.Logger.ErrorContext(c.ctx, "frame observer panic recovered",
				"cp_id", c.id, "panic", r)
		}
	}()
	c.cfg.FrameObserver(c.ctx, ev)
}

func (c *Conn) sampleQueueDepth(depth int) {
	if m, ok := c.cfg.Metrics.(connectionMetricsHook); ok {
		m.OutboundQueueDepth(string(c.version), depth)
	}
}
