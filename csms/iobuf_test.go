package csms

import (
	"bufio"
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

// echoServer registers a raw "Echo" action that returns its payload.
func echoServer(t *testing.T, opts ...Option) (*Server, *httptest.Server) {
	t.Helper()
	srv := NewServer(append([]Option{WithSubProtocols("ocpp1.6")}, opts...)...)
	srv.reg.Register("Echo", func(_ context.Context, _ *dispatcher.Conn, p []byte) ([]byte, error) { return p, nil })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func TestIOBuf_DefaultsAndOption(t *testing.T) {
	require.Equal(t, 1024, NewServer().cfg.ioReadBuffer)
	require.Equal(t, 1024, NewServer().cfg.ioWriteBuffer)
	s := NewServer(WithIOBufferSizes(0, 2048), WithIOBufferSizes(-1, -1))
	require.Equal(t, 0, s.cfg.ioReadBuffer)
	require.Equal(t, 2048, s.cfg.ioWriteBuffer)
}

// The first frame may arrive in the same TCP segment as the handshake: those
// bytes sit in net/http's reader and must reach the websocket reader intact.
func TestIOBuf_FirstFrameInHandshakeSegment(t *testing.T) {
	_, ts := echoServer(t)
	nc, err := net.Dial("tcp", strings.TrimPrefix(ts.URL, "http://"))
	require.NoError(t, err)
	defer nc.Close()

	msg := []byte(`[2,"e1","Echo",{"a":1}]`)
	// Masked client text frame (mask key 0).
	frame := append([]byte{0x81, 0x80 | byte(len(msg)), 0, 0, 0, 0}, msg...)
	req := "GET /ocpp/CP1 HTTP/1.1\r\nHost: x\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\nSec-WebSocket-Version: 13\r\nSec-WebSocket-Protocol: ocpp1.6\r\n\r\n"
	_, err = nc.Write(append([]byte(req), frame...)) // one write: handshake + frame
	require.NoError(t, err)

	br := bufio.NewReader(nc)
	resp, err := http.ReadResponse(br, nil)
	require.NoError(t, err)
	require.Equal(t, 101, resp.StatusCode)
	_ = nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	hdr := make([]byte, 2)
	_, err = br.Read(hdr[:1])
	require.NoError(t, err)
	_, err = br.Read(hdr[1:])
	require.NoError(t, err)
	body := make([]byte, hdr[1]&0x7f)
	n := 0
	for n < len(body) {
		k, err := br.Read(body[n:])
		require.NoError(t, err)
		n += k
	}
	require.JSONEq(t, `[3,"e1",{"a":1}]`, string(body))
}

// Messages far larger than either buffer round-trip in both directions, with and
// without permessage-deflate.
func TestIOBuf_LargeMessagesRoundTrip(t *testing.T) {
	for name, mode := range map[string]transport.CompressionMode{"plain": transport.CompressionDisabled, "deflate": transport.CompressionNoContextTakeover} {
		t.Run(name, func(t *testing.T) {
			_, ts := echoServer(t, WithCompression(mode), WithMaxMessageSize(1<<20))
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CPBIG",
				&websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, CompressionMode: mode.Coder()})
			require.NoError(t, err)
			defer func() { _ = c.CloseNow() }()
			c.SetReadLimit(1 << 20)
			for _, size := range []int{10, 1000, 1500, 64 << 10} {
				pad := strings.Repeat("x", size)
				req := `[2,"b` + strings.Repeat("1", 1) + `","Echo",{"p":"` + pad + `"}]`
				require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(req)))
				_, got, err := c.Read(ctx)
				require.NoError(t, err)
				require.Equal(t, `[3,"b1",{"p":"`+pad+`"}]`, string(got))
			}
		})
	}
}

// The wrapper must keep the Flusher and Unwrap contracts.
func TestIOBuf_WrapperKeepsFlusherAndUnwrap(t *testing.T) {
	rec := httptest.NewRecorder()
	w := newSmallBufWriter(rec, 1024, 1024)
	_, isFlusher := w.(http.Flusher)
	require.True(t, isFlusher)
	u, ok := w.(interface{ Unwrap() http.ResponseWriter })
	require.True(t, ok)
	require.Same(t, rec, u.Unwrap())
	// Zero for both sides means net/http's buffers: no wrapper at all.
	require.Same(t, rec, newSmallBufWriter(rec, 0, 0))
}
