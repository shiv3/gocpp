package msglog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/csms"
)

// Extension adds the message log to serve mode. Every flag is off by
// default; without them nothing is observed and stdout is unchanged.
type Extension struct {
	enabled      bool
	size         int
	file         string
	recordFrames bool
}

// NewExtension returns the message log extension.
func NewExtension() *Extension { return &Extension{} }

// Name implements server.Extension.
func (e *Extension) Name() string { return "message-log" }

// Flags implements server.Extension.
func (e *Extension) Flags(b *config.Binder) {
	b.Bool(&e.enabled, "message-log", false, "keep every OCPP frame (both directions) in an in-memory ring buffer served by GET /messages and GET /messages/stream")
	b.Int(&e.size, "message-log-size", 10000, "entries kept by --message-log")
	b.String(&e.file, "message-log-file", "", "append every OCPP frame (both directions) as one JSON line to this file")
	b.Bool(&e.recordFrames, "record-frames", false, "with --record: record every frame in both directions in wire order (taken from the frame observer) instead of the handled CALLs; lines gain direction, type, msgId and error fields")
}

// recordLine is the stdout line of --record-frames: the --record line plus
// the frame fields.
type recordLine struct {
	Seq              int64           `json:"seq"`
	CPID             string          `json:"cpId"`
	Action           string          `json:"action"`
	Payload          json.RawMessage `json:"payload"`
	Direction        string          `json:"direction"`
	Type             string          `json:"type,omitempty"`
	MsgID            string          `json:"msgId,omitempty"`
	ErrorCode        string          `json:"errorCode,omitempty"`
	ErrorDescription string          `json:"errorDescription,omitempty"`
	ErrorDetails     json.RawMessage `json:"errorDetails,omitempty"`
	Raw              string          `json:"raw,omitempty"`
}

// Setup implements server.Extension.
func (e *Extension) Setup(h *server.Hooks) error {
	if e.size < 1 {
		return errors.New("--message-log-size must be at least 1")
	}
	if e.recordFrames && h.Recorder == nil {
		return errors.New("--record-frames needs --record")
	}
	if !e.enabled && e.file == "" && !e.recordFrames {
		return nil
	}
	log := New()
	ctx, cancel := context.WithCancel(context.Background())

	if e.enabled {
		ring := NewRing(e.size)
		log.AddSink(ring.Add)
		h.Handle("GET /messages", ListHandler(ring, e.size))
		h.Handle("DELETE /messages", ClearHandler(ring))
		h.Handle("GET /messages/stream", StreamHandler(ctx, ring))
	}
	var sink *fileSink
	if e.file != "" {
		f, err := os.OpenFile(e.file, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			cancel()
			return fmt.Errorf("--message-log-file: %w", err)
		}
		sink = &fileSink{f: f, logger: h.Logger}
		log.AddSink(sink.add)
	}
	if e.recordFrames {
		rec := h.Recorder
		h.Inbound.SetRecorder(nil)
		log.AddSink(func(en Entry) {
			_ = rec.RecordLine(func(seq int64) any {
				return recordLine{
					Seq: seq, CPID: en.CPID, Action: en.Action, Payload: en.Payload,
					Direction: en.Direction, Type: en.Type, MsgID: en.MsgID,
					ErrorCode: en.ErrorCode, ErrorDescription: en.ErrorDescription,
					ErrorDetails: en.ErrorDetails, Raw: en.Raw,
				}
			})
		})
	}

	h.FrameObserver(func(_ context.Context, ev observability.FrameEvent) { log.Observe(ev) })
	h.OnDisconnect(func(c *csms.Conn, _ error) { log.Forget(c.ID()) })
	h.OnStart(func(runCtx context.Context, _ *server.Runtime) error {
		go func() {
			<-runCtx.Done()
			cancel()
			if sink != nil {
				sink.close()
			}
		}()
		return nil
	})
	return nil
}

// fileSink appends JSON lines to a file. Lines are written unbuffered so
// nothing is lost when the process stops.
type fileSink struct {
	mu     sync.Mutex
	f      *os.File
	logger *slog.Logger
	failed bool
}

func (s *fileSink) add(e Entry) {
	b, err := json.Marshal(e)
	if err != nil {
		return
	}
	b = append(b, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f == nil {
		return
	}
	if _, err := s.f.Write(b); err != nil && !s.failed {
		s.failed = true
		s.logger.Error("message log file write failed", "error", err)
	}
}

func (s *fileSink) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.f != nil {
		_ = s.f.Close()
		s.f = nil
	}
}
