package csms

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"net/http"
)

// Default per-connection socket buffer sizes. net/http hands a hijacked
// connection a 4 KB reader and a 4 KB writer, which at tens of thousands of
// mostly idle charge points is most of the heap left per connection. OCPP-J
// frames are small and one frame is written per syscall anyway, so 1 KB each is
// enough; larger frames bypass the buffers (one extra syscall for a frame over
// the write buffer size).
const (
	defaultIOReadBuffer  = 1024
	defaultIOWriteBuffer = 1024
	httpDefaultIOBuffer  = 4096 // net/http's own size, used when only one side is overridden
)

// smallBufWriter wraps the ResponseWriter given to websocket.Accept so that
// Hijack returns buffers of the configured sizes instead of net/http's.
type smallBufWriter struct {
	http.ResponseWriter
	rsz, wsz int
}

func newSmallBufWriter(w http.ResponseWriter, read, write int) http.ResponseWriter {
	if read <= 0 && write <= 0 {
		return w
	}
	if read <= 0 {
		read = httpDefaultIOBuffer
	}
	if write <= 0 {
		write = httpDefaultIOBuffer
	}
	return &smallBufWriter{ResponseWriter: w, rsz: read, wsz: write}
}

// Unwrap lets http.ResponseController and the websocket library reach the
// underlying writer (for features other than Hijack).
func (w *smallBufWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Flush keeps http.Flusher working through the wrapper.
func (w *smallBufWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack hijacks the connection and replaces the returned buffers. Bytes net/http
// has already read past the handshake (a client may send its first frame in the
// same segment) are copied and replayed ahead of the connection, and anything
// still pending in the old writer is flushed first, so no byte is lost.
func (w *smallBufWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	// ResponseController follows Unwrap chains, so a user middleware writer that
	// only exposes Unwrap still works (as it does for the websocket library).
	nc, brw, err := http.NewResponseController(w.ResponseWriter).Hijack()
	if err != nil {
		return nil, nil, err
	}
	if brw.Writer.Buffered() > 0 {
		if err := brw.Flush(); err != nil {
			_ = nc.Close()
			return nil, nil, err
		}
	}
	// The websocket library re-wraps the returned reader (it Peeks what is
	// buffered and replays it), so the replayed bytes must already be inside the
	// buffer: read them in now, growing the buffer if they exceed its size.
	rsz := w.rsz
	var r io.Reader = nc
	var pre []byte
	if n := brw.Reader.Buffered(); n > 0 {
		b, _ := brw.Peek(n)
		pre = append([]byte(nil), b...)
		r = io.MultiReader(bytes.NewReader(pre), nc)
		rsz = max(rsz, len(pre))
	}
	br := bufio.NewReaderSize(r, rsz)
	if len(pre) > 0 {
		_, _ = br.Peek(len(pre))
	}
	return nc, bufio.NewReadWriter(br, bufio.NewWriterSize(nc, w.wsz)), nil
}
