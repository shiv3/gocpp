package linknats

import (
	"context"
	"encoding/json"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/shiv3/gocpp/core/observability/tap"
)

// TapOption configures NewTapSink.
type TapOption func(*tapSink)

// WithTapPrefix sets the subject prefix (default "gocpp", the link's default).
func WithTapPrefix(p string) TapOption { return func(s *tapSink) { s.prefix = p } }

type tapSink struct {
	nc     *nats.Conn
	prefix string
}

// NewTapSink returns a tap.Sink that publishes each event as JSON on core NATS:
//
//	<prefix>.tap.frame.<cpID>      frame events
//	<prefix>.tap.session.<cpID>    session lifecycle events
//
// (cpID is made a single subject token like the link's own subjects). Core NATS
// is at most once and a slow subscriber is cut off by the server, which is the
// right failure mode for a tap: the exporter in front of the sink is already
// bounded and lossy, and its Seq numbers expose gaps. To keep events, bind a
// JetStream stream to <prefix>.tap.> (consumers deduplicate by (source, seq)).
// The sink does not own nc: Close flushes it but leaves it open.
func NewTapSink(nc *nats.Conn, opts ...TapOption) tap.Sink {
	s := &tapSink{nc: nc, prefix: DefaultPrefix}
	for _, o := range opts {
		o(s)
	}
	return s
}

func (s *tapSink) WriteBatch(_ context.Context, events []tap.Event) error {
	var firstErr error
	for i := range events {
		data, err := json.Marshal(&events[i])
		if err == nil {
			err = s.nc.Publish(s.prefix+".tap."+string(events[i].Kind)+"."+token(events[i].CPID), data)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (s *tapSink) Close() error { return s.nc.FlushTimeout(2 * time.Second) }
