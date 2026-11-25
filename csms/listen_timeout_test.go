package csms

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

// A peer that opens a connection and never finishes its request headers is cut
// off by ListenAndServe's server instead of being held forever.
func TestListenAndServe_ReadHeaderTimeout(t *testing.T) {
	old := listenReadHeaderTimeout
	listenReadHeaderTimeout = 300 * time.Millisecond
	defer func() { listenReadHeaderTimeout = old }()

	srv := NewServer()
	defer srv.Close()
	hs := srv.newHTTPServer("")
	if hs.ReadHeaderTimeout != 300*time.Millisecond {
		t.Fatalf("ReadHeaderTimeout = %v", hs.ReadHeaderTimeout)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { defer close(done); _ = hs.Serve(ln) }()
	defer func() { _ = hs.Close(); <-done }()

	nc, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = nc.Close() }()
	_, _ = nc.Write([]byte("GET /ocpp/x HTTP/1.1\r\nHost: x\r\n")) // headers never end
	_ = nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	start := time.Now()
	_, err = io.ReadAll(nc) // the server answers 408 and closes
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("the connection with unfinished headers was still open after 5 s")
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("took %v to cut off the slow peer", d)
	}
}
