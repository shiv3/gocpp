package observability

import (
	"context"
	"log/slog"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
)

// FrameDirection says whether a frame was received or sent by this peer.
type FrameDirection string

const (
	FrameIn  FrameDirection = "in"
	FrameOut FrameDirection = "out"
)

// FrameEvent describes one raw OCPP-J frame crossing the WebSocket.
type FrameEvent struct {
	CPID      string
	Version   ocppj.Version
	Direction FrameDirection
	// Raw is the frame exactly as read or written (including signed-message
	// wrappers). It is shared with the connection: do not modify it, and copy
	// it if it must outlive the callback.
	Raw []byte
	// Type, MsgID and Action are best-effort. For an unparsable inbound frame
	// they are filled only as far as they could be decoded (Type is 0 when not
	// even that is readable). Action is empty for CALLRESULT / CALLERROR /
	// CALLRESULTERROR, which carry none.
	Type   ocppj.MessageType
	MsgID  string
	Action string
	Time   time.Time
	// Err is the write error for an outbound frame that failed to be written;
	// nil otherwise (and always nil inbound).
	Err error
}

// FrameObserver receives every raw frame. It runs inline on the connection's
// reader (inbound) or writer (outbound) goroutine, so it must return quickly:
// a slow observer delays all traffic on that connection. Hand work off to a
// channel or goroutine if it may block. Panics are recovered and logged.
type FrameObserver func(ctx context.Context, ev FrameEvent)

// SlogFrameObserver returns a FrameObserver that logs each frame to logger at
// level with the attributes cp_id, direction, action, msg_id and frame (the raw
// text, truncated to maxRaw bytes; maxRaw <= 0 means no truncation). A failed
// write is logged at slog.LevelWarn with the error.
func SlogFrameObserver(logger *slog.Logger, level slog.Level, maxRaw int) FrameObserver {
	if logger == nil {
		logger = slog.Default()
	}
	return func(ctx context.Context, ev FrameEvent) {
		raw := ev.Raw
		truncated := false
		if maxRaw > 0 && len(raw) > maxRaw {
			raw, truncated = raw[:maxRaw], true
		}
		attrs := []slog.Attr{
			slog.String(AttrCPID, ev.CPID),
			slog.String(AttrDirection, string(ev.Direction)),
			slog.String(AttrAction, ev.Action),
			slog.String(AttrMsgID, ev.MsgID),
			slog.String("frame", string(raw)),
		}
		if truncated {
			attrs = append(attrs, slog.Bool("truncated", true), slog.Int("frame_len", len(ev.Raw)))
		}
		lvl := level
		if ev.Err != nil {
			attrs = append(attrs, slog.Any(AttrError, ev.Err))
			lvl = slog.LevelWarn
		}
		logger.LogAttrs(ctx, lvl, "ocpp frame", attrs...)
	}
}
