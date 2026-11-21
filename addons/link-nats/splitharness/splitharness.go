// Package splitharness runs the repository's conformance and e2e suites in
// split mode over the NATS link: every server a test creates becomes a gateway
// and a processing tier that talk through a real (embedded) nats-server on
// loopback instead of the in-process link.
//
// The suites live in the main module, which must not depend on this one, so the
// factory is plugged in from outside: scripts/split-nats-suite.sh builds a
// throwaway go.work and an overlay file for internal/splitmode that registers
// Link with it; run that script (CI does) rather than importing this package.
package splitharness

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	natsd "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
)

var (
	mu     sync.Mutex
	ns     *natsd.Server
	users  int
	linkID atomic.Int64
)

// acquire returns the shared embedded NATS server, starting it for the first
// user. The server is stopped again when the last link is released, so a test
// binary that verifies its goroutines at exit (goleak) finds none of the
// broker's. Every link gets its own subject prefix, so servers of different
// tests never see each other's traffic.
func acquire() (*natsd.Server, error) {
	mu.Lock()
	defer mu.Unlock()
	if users == 0 {
		srv, err := natsd.NewServer(&natsd.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true})
		if err != nil {
			return nil, err
		}
		go srv.Start()
		if !srv.ReadyForConnections(10 * time.Second) {
			srv.Shutdown()
			return nil, fmt.Errorf("splitharness: embedded nats-server did not start")
		}
		ns = srv
	}
	users++
	return ns, nil
}

func release() {
	mu.Lock()
	defer mu.Unlock()
	if users--; users == 0 {
		ns.Shutdown()
		ns.WaitForShutdown()
		ns = nil
	}
}

// Link is a csms.SplitLink over NATS.
type Link struct {
	gw    *linknats.Gateway
	be    *linknats.Backend
	conns []*nats.Conn
}

var _ csms.SplitLink = (*Link)(nil)

// New returns a fresh NATS link with its own subject prefix, or panics: it is
// meant for test setup, where a broken broker is a failed run.
func New() *Link {
	srv, err := acquire()
	if err != nil {
		panic(err)
	}
	prefix := "splt" + strconv.FormatInt(linkID.Add(1), 10)
	connect := func() *nats.Conn {
		nc, err := nats.Connect(srv.ClientURL(), nats.ReconnectBufSize(-1), nats.NoReconnect())
		if err != nil {
			panic(err)
		}
		return nc
	}
	l := &Link{conns: []*nats.Conn{connect(), connect()}}
	if l.gw, err = linknats.NewGateway(l.conns[0], linknats.WithPrefix(prefix), linknats.WithUpstreamWait(5*time.Second)); err != nil {
		panic(err)
	}
	if l.be, err = linknats.NewBackend(l.conns[1], linknats.WithBackendPrefix(prefix),
		linknats.WithHelloInterval(100*time.Millisecond), linknats.WithDrainTimeout(2*time.Second)); err != nil {
		panic(err)
	}
	return l
}

// Gateway implements csms.SplitLink.
func (l *Link) Gateway() gateway.GatewayLink { return l.gw }

// Backend implements csms.SplitLink.
func (l *Link) Backend() gateway.BackendLink { return l.be }

// AttachBackend implements csms.SplitLink: it serves up in the background and
// returns once the gateway lists the backend. The returned function drains the
// backend and releases the link, its connections included.
func (l *Link) AttachBackend(up gateway.Upstream, info gateway.BackendInfo) func(context.Context) error {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- l.be.Serve(ctx, up, info) }()
	pool := info.Pool
	if pool == "" {
		pool = gateway.DefaultPool
	}
	deadline := time.Now().Add(10 * time.Second)
	for !l.gw.Available(pool) && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	var once sync.Once
	return func(dctx context.Context) error {
		var err error
		once.Do(func() {
			cancel()
			select {
			case err = <-done:
			case <-dctx.Done():
				err = dctx.Err()
			}
			l.be.Close()
			l.gw.Close()
			for _, nc := range l.conns {
				nc.Close()
			}
			release()
		})
		return err
	}
}
