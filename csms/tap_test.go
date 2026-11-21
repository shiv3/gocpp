package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/observability/tap"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type tapSink struct {
	mu sync.Mutex
	ev []tap.Event
}

func (s *tapSink) WriteBatch(_ context.Context, ev []tap.Event) error {
	s.mu.Lock()
	s.ev = append(s.ev, ev...)
	s.mu.Unlock()
	return nil
}
func (s *tapSink) Close() error { return nil }
func (s *tapSink) snapshot() []tap.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tap.Event(nil), s.ev...)
}

func (s *tapSink) sessionEvents(kind string) (n int) {
	for _, e := range s.snapshot() {
		if e.Kind == tap.KindSession && e.Session == kind {
			n++
		}
	}
	return n
}

// The tap sees both frames of a Heartbeat and the opened/closed events of the
// session, on an embedded server and on a gateway (split in-process), where the
// session events come from the socket-holding tier only.
func TestTap_FramesAndSessions(t *testing.T) {
	for name, split := range map[string]bool{"embedded": false, "gateway": true} {
		t.Run(name, func(t *testing.T) {
			defer goleak.VerifyNone(t)
			sink := &tapSink{}
			ex := tap.New(sink, tap.WithSource("t"), tap.WithBatch(1, time.Hour))
			opts := []csms.Option{
				csms.WithFrameObserver(ex.FrameObserver()), csms.WithSessionObserver(ex.SessionObserver()),
				csms.WithSessionAttributes(func(*http.Request, string) map[string]string { return map[string]string{"tenant": "a"} }),
			}
			if split {
				opts = append(opts, csms.WithSplitInProcess())
			}
			srv := csms.NewServer(opts...)
			require.NoError(t, csms.On(srv, edgeHBMsg, func(context.Context, *csms.Conn, edgeHB) (edgeHBResp, error) {
				return edgeHBResp{CurrentTime: "now"}, nil
			}))
			ts := httptest.NewServer(srv.Handler())
			url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_T"
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
			require.NoError(t, err)
			arr := rawCall(t, c, `[2,"h1","Heartbeat",{}]`)
			require.Equal(t, "3", string(arr[0]))
			require.NoError(t, c.Close(websocket.StatusNormalClosure, ""))
			require.Eventually(t, func() bool { return sink.sessionEvents("closed") == 1 }, 3*time.Second, 10*time.Millisecond)
			ts.Close()
			srv.Close()
			require.NoError(t, ex.Close(context.Background()))

			var in, out int
			for _, e := range sink.snapshot() {
				if e.Kind == tap.KindFrame && e.CPID == "CP_T" && e.Action == "Heartbeat" {
					if e.Direction == string(observability.FrameIn) {
						in++
					}
				}
				if e.Kind == tap.KindFrame && e.Direction == string(observability.FrameOut) && e.MsgType == 3 {
					out++
				}
			}
			require.Equal(t, 1, in, "the CALL")
			require.Equal(t, 1, out, "the CALLRESULT")
			require.Equal(t, 1, sink.sessionEvents("opened"))
			require.Equal(t, 1, sink.sessionEvents("closed"), "closed fires once")
			for _, e := range sink.snapshot() {
				if e.Kind == tap.KindSession {
					require.Equal(t, "CP_T", e.CPID)
					require.Equal(t, "1.6", e.Version)
					require.Equal(t, "a", e.Attrs["tenant"])
				}
			}
		})
	}
}

// A charger reconnecting over its own live socket yields exactly one closed
// event for the displaced session, with the replacement cause.
func TestTap_ReplacedSessionClosesOnce(t *testing.T) {
	defer goleak.VerifyNone(t)
	sink := &tapSink{}
	ex := tap.New(sink, tap.WithBatch(1, time.Hour))
	srv := csms.NewServer(csms.WithSessionObserver(ex.SessionObserver()))
	ts := httptest.NewServer(srv.Handler())
	url := "ws" + ts.URL[len("http"):] + "/ocpp/CP_R"
	dial := func() *websocket.Conn {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
		require.NoError(t, err)
		return c
	}
	c1 := dial()
	c2 := dial()
	require.Eventually(t, func() bool { return sink.sessionEvents("closed") == 1 }, 3*time.Second, 10*time.Millisecond)
	_ = c1.CloseNow()
	_ = c2.CloseNow()
	require.Eventually(t, func() bool { return sink.sessionEvents("closed") == 2 }, 3*time.Second, 10*time.Millisecond)
	ts.Close()
	srv.Close()
	require.NoError(t, ex.Close(context.Background()))
	require.Equal(t, 2, sink.sessionEvents("opened"))
	require.Equal(t, 2, sink.sessionEvents("closed"))
	var replaced int
	for _, e := range sink.snapshot() {
		if e.Session == "closed" && e.Cause != "" {
			replaced++
		}
	}
	require.GreaterOrEqual(t, replaced, 1)
}
