// Package servertest starts a serve-mode gocpp-csms with extensions in a
// test, from command line arguments, the way "gocpp-csms serve" does. It is
// imported by tests only.
package servertest

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/config"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/server"
	"github.com/shiv3/gocpp/cp"
)

// Buffer is a goroutine-safe stdout.
type Buffer struct {
	mu sync.Mutex
	b  strings.Builder
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}

// String returns what was written.
func (b *Buffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.String()
}

// Server is a started test server.
type Server struct {
	*server.Server
	T      *testing.T
	Stdout *Buffer
	stop   sync.Once
}

// Parse binds the serve flags and those of exts and parses args (ephemeral
// loopback ports unless given). It returns the config or the setup error.
func Parse(args []string, exts ...server.Extension) (*config.Config, error) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	b := config.NewBinder(fs, nil)
	cfg := config.Bind(b)
	for _, e := range exts {
		e.Flags(b)
	}
	base := []string{"--ocpp-addr", "127.0.0.1:0", "--control-addr", "127.0.0.1:0", "--drain-timeout", "0s", "--command-timeout", "3s"}
	if err := fs.Parse(append(base, args...)); err != nil {
		return nil, err
	}
	return cfg, nil
}

// New builds (without starting) a server; the error is New's.
func New(t *testing.T, args []string, exts ...server.Extension) (*server.Server, *Buffer, error) {
	t.Helper()
	cfg, err := Parse(args, exts...)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	if testing.Verbose() {
		logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	out := &Buffer{}
	s, err := server.New(cfg, logger, out, exts...)
	return s, out, err
}

// Start builds and starts a server, stopped at the end of the test.
func Start(t *testing.T, args []string, exts ...server.Extension) *Server {
	t.Helper()
	s, out, err := New(t, args, exts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Start(); err != nil {
		t.Fatal(err)
	}
	ts := &Server{Server: s, T: t, Stdout: out}
	t.Cleanup(ts.Stop)
	return ts
}

// Stop shuts the server down (idempotent).
func (s *Server) Stop() {
	s.stop.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.Shutdown(ctx); err != nil {
			s.T.Errorf("shutdown: %v", err)
		}
	})
}

// URL is the ws:// URL of path on the OCPP listener.
func (s *Server) URL(path string) string { return fmt.Sprintf("ws://%s%s", s.OCPPAddr(), path) }

// Dial connects a 1.6 charge point to path; the client is closed at the end
// of the test.
func (s *Server) Dial(cpID, path string, opts ...cp.Option) (*cp.Client, error) {
	opts = append([]cp.Option{cp.WithSubProtocols("ocpp1.6")}, opts...)
	c := cp.NewClient(cpID, s.URL(path), opts...)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		return nil, err
	}
	s.T.Cleanup(c.Close)
	return c, nil
}

// MustDial is Dial failing the test on error.
func (s *Server) MustDial(cpID, path string, opts ...cp.Option) *cp.Client {
	s.T.Helper()
	c, err := s.Dial(cpID, path, opts...)
	if err != nil {
		s.T.Fatalf("connect %s: %v", cpID, err)
	}
	return c
}

// Control sends a control API request; header is "Name: value" pairs.
func (s *Server) Control(method, path, body string, header ...string) (int, string) {
	s.T.Helper()
	req, err := http.NewRequest(method, "http://"+s.ControlAddr().String()+path, strings.NewReader(body))
	if err != nil {
		s.T.Fatal(err)
	}
	for _, h := range header {
		k, v, _ := strings.Cut(h, ":")
		req.Header.Set(k, strings.TrimSpace(v))
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.T.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, strings.TrimSuffix(string(b), "\n")
}

// Call sends a raw CALL from c and returns the answer.
func Call(t *testing.T, c *cp.Client, action, payload string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := cp.CallRaw(ctx, c, action, []byte(payload))
	if err != nil {
		t.Fatalf("%s: %v", action, err)
	}
	return string(resp)
}

// WaitFor polls cond until it holds or 5 s passed.
func WaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
