package dispatcher

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
	"golang.org/x/sync/semaphore"
)

// Conn is one OCPP connection. It owns reader, writer, and dispatcher goroutines
// whose lifetime is bound to ctx. All teardown flows from cancel(cause).
type Conn struct {
	id      string
	ws      transport.WS
	version ocppj.Version
	meta    ConnMetadata

	out chan outbound
	in  chan ocppj.Frame

	// activity signals inbound frames (data/ping/pong) to the read idle watchdog.
	activity chan struct{}

	pending *pendingStore
	reg     *HandlerRegistry
	sem     *semaphore.Weighted
	outSem  *semaphore.Weighted

	ctx     context.Context
	cancel  context.CancelCauseFunc
	closeWS func()
	done    chan struct{}
	wg      sync.WaitGroup

	// Async outbound call machinery (DoCallAsync). asyncWG tracks the worker and
	// any concurrent async goroutines; asyncClosed (under asyncMu) gates new ones
	// so Add can never race Close's Wait.
	asyncMu     sync.Mutex
	asyncClosed bool
	asyncQ      chan asyncJob
	asyncWG     sync.WaitGroup

	cfg Config
}

type outbound struct {
	payload []byte
	sentCh  chan error // 1-buffered
}

// ConnMetadata contains HTTP upgrade metadata associated with a connection.
// Fields are empty for connections that were not created from an inbound HTTP
// upgrade request.
type ConnMetadata struct {
	RemoteAddr    string
	RequestHeader http.Header
	TLS           *tls.ConnectionState
}

// NewConn creates a connection. Call Start to launch its goroutines.
func NewConn(id string, ws transport.WS, cfg Config, reg *HandlerRegistry, meta ...ConnMetadata) *Conn {
	m := ConnMetadata{}
	if len(meta) > 0 {
		m = cloneConnMetadata(meta[0])
	}
	c := &Conn{
		id:       id,
		ws:       ws,
		version:  subprotocolToVersion(ws.Subprotocol()),
		meta:     m,
		out:      make(chan outbound, cfg.OutboundQueueSize),
		in:       make(chan ocppj.Frame, cfg.OutboundQueueSize),
		activity: make(chan struct{}, 1),
		pending:  newPendingStore(),
		reg:      reg,
		sem:      semaphore.NewWeighted(cfg.MaxConcurrentHandlers),
		outSem:   semaphore.NewWeighted(1),
		done:     make(chan struct{}),
		cfg:      cfg,
	}
	c.closeWS = sync.OnceFunc(func() { _ = ws.Close(transport.StatusNormalClosure, "closed") })
	return c
}

// ID returns the charge point identifier.
func (c *Conn) ID() string { return c.id }

// Version returns the negotiated OCPP version.
func (c *Conn) Version() ocppj.Version { return c.version }

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *Conn) Subprotocol() string { return c.ws.Subprotocol() }

// RemoteAddr returns the peer network address from the HTTP upgrade request, if
// available.
func (c *Conn) RemoteAddr() string { return c.meta.RemoteAddr }

// RequestHeader returns a copy of the HTTP upgrade request headers.
func (c *Conn) RequestHeader() http.Header { return c.meta.RequestHeader.Clone() }

// TLS returns a copy of the HTTP upgrade TLS connection state, if available.
func (c *Conn) TLS() *tls.ConnectionState {
	if c.meta.TLS == nil {
		return nil
	}
	state := *c.meta.TLS
	return &state
}

// Context returns the connection lifecycle context.
func (c *Conn) Context() context.Context { return c.ctx }

// NoteActivity records inbound activity, resetting the read idle watchdog.
// It is safe to call from the websocket read goroutine (e.g. ping/pong
// callbacks), as the send is non-blocking.
func (c *Conn) NoteActivity() {
	select {
	case c.activity <- struct{}{}:
	default:
	}
}

func cloneConnMetadata(meta ConnMetadata) ConnMetadata {
	cloned := ConnMetadata{
		RemoteAddr:    meta.RemoteAddr,
		RequestHeader: meta.RequestHeader.Clone(),
	}
	if meta.TLS != nil {
		state := *meta.TLS
		cloned.TLS = &state
	}
	return cloned
}

// Start launches the connection goroutines bound to parent.
func (c *Conn) Start(parent context.Context) {
	c.ctx, c.cancel = context.WithCancelCause(parent)
	c.cfg.Metrics.ConnectionOpened()

	c.wg.Add(3)
	go c.reader()
	go c.writer()
	go c.dispatch()
	c.startWebSocketPing()
	c.startReadWatchdog()

	go func() {
		c.wg.Wait()
		close(c.done)
	}()
}

// Close tears down the connection. Safe to call multiple times.
func (c *Conn) Close(reason error) error {
	if reason == nil {
		reason = ocppj.ErrConnClosed
	}
	if c.cancel != nil {
		c.cancel(reason)
	}
	<-c.done
	c.closeWS()
	c.pending.failAll(context.Cause(c.ctx))
	// Stop accepting new async calls, then wait for the worker + any in-flight
	// async goroutines to drain (their DoCalls have already failed via ctx/pending).
	c.asyncMu.Lock()
	c.asyncClosed = true
	c.asyncMu.Unlock()
	c.asyncWG.Wait()
	c.cfg.Metrics.ConnectionClosed()
	return nil
}

func (c *Conn) reader() {
	defer c.wg.Done()
	for {
		msg, err := c.ws.Read(c.ctx)
		if err != nil {
			c.cancel(fmt.Errorf("read: %w", err))
			return
		}
		c.NoteActivity()
		frame, err := ocppj.Parse(msg)
		if err != nil {
			if errors.Is(err, ocppj.ErrIgnoredMessageType) {
				c.cfg.Logger.DebugContext(c.ctx, "ignored unknown message type",
					"cp_id", c.id, "err", err)
				continue
			}
			c.cfg.Logger.WarnContext(c.ctx, "ocpp parse error",
				"cp_id", c.id, "err", err)
			continue
		}
		if !c.routeInbound(frame) {
			return
		}
	}
}

// routeInbound dispatches a parsed frame. It returns false when the connection
// context is done and the reader must stop.
func (c *Conn) routeInbound(frame ocppj.Frame) bool {
	switch frame.Type {
	case ocppj.CallResult:
		c.pending.resolve(frame.MsgID, rawResult{payload: frame.Payload})
	case ocppj.MessageTypeCallError:
		ce := &ocppj.CallError{Code: ocppj.ErrorCode(frame.ErrCode), Description: frame.ErrDesc}
		c.pending.resolve(frame.MsgID, rawResult{err: ce})
	case ocppj.MessageTypeCallResultError:
		ce := &ocppj.CallError{Code: ocppj.ErrorCode(frame.ErrCode), Description: frame.ErrDesc, IsResultError: true}
		c.pending.resolve(frame.MsgID, rawResult{err: ce})
	case ocppj.Call, ocppj.Send:
		select {
		case c.in <- frame:
		case <-c.ctx.Done():
			return false
		}
	}
	return true
}

func (c *Conn) writer() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case ob := <-c.out:
			wctx, cancel := context.WithTimeout(c.ctx, c.cfg.WriteTimeout)
			err := c.ws.Write(wctx, ob.payload)
			cancel()
			ob.sentCh <- err // 1-buffered
			if err != nil {
				c.cancel(fmt.Errorf("write: %w", err))
				return
			}
		}
	}
}

func (c *Conn) dispatch() {
	defer c.wg.Done()
	for {
		select {
		case <-c.ctx.Done():
			return
		case frame := <-c.in:
			if err := c.sem.Acquire(c.ctx, 1); err != nil {
				return
			}
			if c.cfg.GlobalHandlerLimiter != nil {
				if err := c.cfg.GlobalHandlerLimiter.Acquire(c.ctx, 1); err != nil {
					c.sem.Release(1)
					return
				}
			}
			go c.runHandler(frame)
		}
	}
}

func subprotocolToVersion(sub string) ocppj.Version {
	switch sub {
	case "ocpp1.6":
		return ocppj.V16
	case "ocpp2.0.1":
		return ocppj.V201
	case "ocpp2.1":
		return ocppj.V21
	default:
		return ocppj.Version(sub)
	}
}
