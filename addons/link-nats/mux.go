package linknats

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/shiv3/gocpp/gateway"
)

// Headers on the NATS messages. Bodies are gocpp.link.v1 protobuf messages.
const (
	hAccepted    = "Gocpp-Accepted"    // interim reply: the id of the peer that took the request
	hUnsupported = "Gocpp-Unsupported" // reply: the request is of a kind the peer does not know
	hBackend     = "Gocpp-Backend"     // request from a backend: its id
)

var (
	// errNotPublished: the request never left this process.
	errNotPublished = fmt.Errorf("%w: not connected to NATS", gateway.ErrNotDelivered)
	// errNoResponders: the broker reports nobody is subscribed.
	errNoResponders = fmt.Errorf("%w: no responders", gateway.ErrNotDelivered)
	// errPeerLost: the peer working on the request stopped announcing itself.
	errPeerLost = errors.New("linknats: peer lost")
	// errNoAccept: no peer acknowledged the request in time; it may or may not
	// have reached one, so it is not redispatched.
	errNoAccept = errors.New("linknats: request not acknowledged by any backend")
	// errConnLost: the NATS connection dropped while the request was out.
	errConnLost = errors.New("linknats: connection to NATS lost")
)

// token makes s usable as one subject token.
func token(s string) string {
	if s == "" {
		return "_"
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '.', '*', '>', ' ', '\t', '\r', '\n':
			return '_'
		}
		return r
	}, s)
}

// pend is one request waiting for its answer.
type pend struct {
	ch   chan *nats.Msg
	lost chan error
	exec atomic.Pointer[string] // the peer working on it, once known
}

// mux correlates replies to requests over one wildcard reply subscription. It
// exists instead of nats.Conn.Request because a request here can get an interim
// "accepted" frame before the answer, and because peers failing (lease lapse,
// connection loss) must fail the requests they hold.
type mux struct {
	nc     *nats.Conn
	prefix string // reply subject prefix, with trailing dot
	sub    *nats.Subscription
	stop   chan struct{}
	wg     sync.WaitGroup

	// acceptTimeout bounds how long a request addressed to a queue group may go
	// unacknowledged (0: no bound beyond the caller's deadline).
	acceptTimeout time.Duration

	mu     sync.Mutex
	next   uint64
	m      map[uint64]*pend
	closed bool
}

func newMux(nc *nats.Conn, subjectPrefix, owner string) (*mux, error) {
	x := &mux{nc: nc, prefix: subjectPrefix + ".r." + owner + ".", m: make(map[uint64]*pend), stop: make(chan struct{})}
	sub, err := nc.Subscribe(x.prefix+"*", x.deliver)
	if err != nil {
		return nil, err
	}
	sub.SetPendingLimits(-1, -1)
	x.sub = sub
	x.wg.Add(1)
	go x.watchConn()
	return x, nil
}

func (x *mux) deliver(m *nats.Msg) {
	id, err := strconv.ParseUint(strings.TrimPrefix(m.Subject, x.prefix), 10, 64)
	if err != nil {
		return
	}
	x.mu.Lock()
	p := x.m[id]
	x.mu.Unlock()
	if p == nil {
		return
	}
	select {
	case p.ch <- m:
	default: // an interim frame nobody needs
	}
}

// watchConn fails every request when the NATS connection drops: what was in
// flight may or may not have reached its peer, and a reconnect does not bring
// the answers back.
func (x *mux) watchConn() {
	defer x.wg.Done()
	t := time.NewTicker(50 * time.Millisecond)
	defer t.Stop()
	was := x.nc.Stats().Reconnects
	up := x.nc.IsConnected()
	for {
		select {
		case <-x.stop:
			return
		case <-t.C:
		}
		now := x.nc.IsConnected()
		rc := x.nc.Stats().Reconnects
		if (up && !now) || rc != was {
			x.failWhere(func(string) bool { return true }, errConnLost)
		}
		up, was = now, rc
	}
}

// failWhere fails the requests whose executor satisfies pred.
func (x *mux) failWhere(pred func(exec string) bool, err error) {
	x.mu.Lock()
	var hit []*pend
	for _, p := range x.m {
		e := ""
		if s := p.exec.Load(); s != nil {
			e = *s
		}
		if pred(e) {
			hit = append(hit, p)
		}
	}
	x.mu.Unlock()
	for _, p := range hit {
		select {
		case p.lost <- err:
		default:
		}
	}
}

// busyWith reports whether a request executed by exec is still waiting.
func (x *mux) busyWith(exec string) bool {
	x.mu.Lock()
	defer x.mu.Unlock()
	for _, p := range x.m {
		if s := p.exec.Load(); s != nil && *s == exec {
			return true
		}
	}
	return false
}

func (x *mux) add(exec string) (uint64, *pend, bool) {
	x.mu.Lock()
	defer x.mu.Unlock()
	if x.closed {
		return 0, nil, false
	}
	x.next++
	p := &pend{ch: make(chan *nats.Msg, 8), lost: make(chan error, 1)}
	if exec != "" {
		p.exec.Store(&exec)
	}
	x.m[x.next] = p
	return x.next, p, true
}

func (x *mux) drop(id uint64) {
	x.mu.Lock()
	delete(x.m, id)
	x.mu.Unlock()
}

// request publishes data on subject and waits for the final answer. exec, when
// set, names the peer that will work on it (a request addressed to one peer);
// otherwise the peer announces itself in an interim frame. The returned
// executor is the last one known.
func (x *mux) request(ctx context.Context, subject string, hdr nats.Header, data []byte, exec string) (*nats.Msg, string, error) {
	if !x.nc.IsConnected() {
		return nil, "", errNotPublished
	}
	id, p, ok := x.add(exec)
	if !ok {
		return nil, "", errNotPublished
	}
	defer x.drop(id)
	msg := &nats.Msg{Subject: subject, Reply: x.prefix + strconv.FormatUint(id, 10), Header: hdr, Data: data}
	if err := x.nc.PublishMsg(msg); err != nil {
		return nil, "", fmt.Errorf("%w: %w", errNotPublished, err)
	}
	var accept <-chan time.Time
	if exec == "" && x.acceptTimeout > 0 {
		t := time.NewTimer(x.acceptTimeout)
		defer t.Stop()
		accept = t.C
	}
	for {
		select {
		case <-accept:
			accept = nil
			if p.exec.Load() == nil {
				return nil, "", errNoAccept
			}
		case m := <-p.ch:
			if m.Header.Get("Status") == "503" && len(m.Data) == 0 {
				return nil, "", errNoResponders
			}
			if a := m.Header.Get(hAccepted); a != "" {
				p.exec.Store(&a)
				accept = nil
				continue
			}
			e := ""
			if s := p.exec.Load(); s != nil {
				e = *s
			}
			return m, e, nil
		case err := <-p.lost:
			e := ""
			if s := p.exec.Load(); s != nil {
				e = *s
			}
			return nil, e, err
		case <-ctx.Done():
			e := ""
			if s := p.exec.Load(); s != nil {
				e = *s
			}
			return nil, e, ctx.Err()
		}
	}
}

func (x *mux) close() {
	x.mu.Lock()
	if x.closed {
		x.mu.Unlock()
		return
	}
	x.closed = true
	x.mu.Unlock()
	close(x.stop)
	x.wg.Wait()
	_ = x.sub.Unsubscribe()
	x.failWhere(func(string) bool { return true }, errConnLost)
}

// respond answers m with data; the error of a publish on a dying connection is
// not actionable.
func respond(nc *nats.Conn, m *nats.Msg, data []byte, hdr nats.Header) {
	if m.Reply == "" {
		return
	}
	_ = nc.PublishMsg(&nats.Msg{Subject: m.Reply, Data: data, Header: hdr})
}

func respondUnsupported(nc *nats.Conn, m *nats.Msg, what string) {
	respond(nc, m, nil, nats.Header{hUnsupported: []string{what}})
}

func accepted(nc *nats.Conn, m *nats.Msg, id string) {
	respond(nc, m, nil, nats.Header{hAccepted: []string{id}})
}
