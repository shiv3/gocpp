package tap

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"os"
	"sync"
)

// jsonlSink writes one JSON object per line.
type jsonlSink struct {
	mu sync.Mutex
	w  *bufio.Writer
	c  io.Closer
}

// JSONL returns a Sink that appends one JSON object per line to w. If w is also
// an io.Closer it is closed with the sink. Writes are buffered per batch and
// flushed after each one, so a crash loses at most the batch in progress.
func JSONL(w io.Writer) Sink {
	s := &jsonlSink{w: bufio.NewWriterSize(w, 64<<10)}
	if c, ok := w.(io.Closer); ok {
		s.c = c
	}
	return s
}

// JSONLFile opens path for appending (created with mode 0600) and returns a
// JSONL sink on it. Rotation is left to the platform (logrotate with copytruncate,
// or a sidecar), as for any log file.
func JSONLFile(path string) (Sink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return JSONL(f), nil
}

func (s *jsonlSink) WriteBatch(_ context.Context, events []Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	enc := json.NewEncoder(s.w) // Encode appends the newline
	for i := range events {
		if err := enc.Encode(&events[i]); err != nil {
			return err
		}
	}
	return s.w.Flush()
}

func (s *jsonlSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	err := s.w.Flush()
	if s.c != nil {
		if cerr := s.c.Close(); err == nil {
			err = cerr
		}
	}
	return err
}

type multiSink []Sink

// Multi returns a Sink that writes every batch to each of sinks, in order. A
// failing sink does not stop the others; the first error is returned.
func Multi(sinks ...Sink) Sink { return multiSink(sinks) }

func (m multiSink) WriteBatch(ctx context.Context, events []Event) error {
	var first error
	for _, s := range m {
		if err := s.WriteBatch(ctx, events); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func (m multiSink) Close() error {
	var first error
	for _, s := range m {
		if err := s.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}
