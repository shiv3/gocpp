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

	ctx         context.Context
	cancel      context.CancelCauseFunc
	closeWSOnce sync.Once
	done        chan struct{}
	wg          sync.WaitGroup

	// Async outbound call machinery (DoCallAsync). asyncWG tracks the worker and
	// any concurrent async goroutines; asyncClosed (under asyncMu) gates new ones
	// so Add can never race Close's Wait.
	asyncMu     sync.Mutex
	asyncClosed bool
	asyncQ      chan asyncJob
	asyncWG     sync.WaitGroup

	// inboundGate, when non-nil, holds inbound CALL/SEND dispatch until closed
	// (see HoldInbound). The reader keeps reading and resolving CALLRESULTs.
	inboundGate     chan struct{}
	inboundGateOnce sync.Once

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
	return c
}

// HoldInbound makes the connection withhold inbound CALL and SEND handling until
// ReleaseInbound is called. Call it before Start. The server side uses it so no
// handler runs before the connection is registered and its connect callback has
// returned. Connections that never call it dispatch immediately.
func (c *Conn) HoldInbound() { c.inboundGate = make(chan struct{}) }

// ReleaseInbound opens the gate armed by HoldInbound. Idempotent; a no-op when
// the gate was never armed. Teardown also unblocks the waiting dispatcher.
func (c *Conn) ReleaseInbound() {
	if c.inboundGate != nil {
		c.inboundGateOnce.Do(func() { close(c.inboundGate) })
	}
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

// replacedCloseReason is the WebSocket close reason sent to a connection that
// was superseded by a newer one with the same id.
const replacedCloseReason = "replaced by a new connection with the same ID"

// closedByServerReason is the WebSocket close reason sent when the server
// closes the connection on purpose.
const closedByServerReason = "closed by the server"

// closeWS closes the WebSocket with a close frame. The first call decides the
// code; later calls are no-ops.
func (c *Conn) closeWS(code transport.StatusCode, reason string) {
	c.closeWSOnce.Do(func() { _ = c.ws.Close(code, reason) })
}

// closer waits for teardown and then closes the socket gracefully. The reader
// and writer deliberately do not abort the socket on cancellation (the websocket
// library drops the TCP connection without a close frame when an in-flight
// operation's ctx is cancelled), so this goroutine is what tells the peer why.
func (c *Conn) closer() {
	defer c.wg.Done()
	<-c.ctx.Done()
	if errors.Is(context.Cause(c.ctx), ocppj.ErrConnReplaced) {
		c.closeWS(transport.StatusPolicyViolation, replacedCloseReason)
		return
	}
	if errors.Is(context.Cause(c.ctx), ocppj.ErrConnClosedByServer) {
		c.closeWS(transport.StatusNormalClosure, closedByServerReason)
		return
	}
	c.closeWS(transport.StatusNormalClosure, "closed")
}

// Start launches the connection goroutines bound to parent.
func (c *Conn) Start(parent context.Context) {
	c.ctx, c.cancel = context.WithCancelCause(parent)
	c.cfg.Metrics.ConnectionOpened()

	c.wg.Add(4)
	go c.closer()
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

// Abort cancels the connection with cause without waiting for teardown. The
// socket is then closed in the background with a close frame matching cause
// (see closer); call Close afterwards to wait for completion.
func (c *Conn) Abort(cause error) {
	if cause == nil {
		cause = ocppj.ErrConnClosed
	}
	if c.cancel != nil {
		c.cancel(cause)
	}
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
	c.closeWS(transport.StatusNormalClosure, "closed")
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
		// Read with a ctx that survives cancellation: cancelling an in-flight read
		// makes the websocket library drop the TCP connection without a close
		// frame. The closer goroutine closes the socket, which ends this read.
		msg, err := c.ws.Read(context.WithoutCancel(c.ctx))
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
				// OCPP 2.0.1 / 2.1 Part 4 §4.2.3: a message with a MessageTypeId the
				// receiver does not support is answered with CALLERROR
				// MessageTypeNotSupported. OCPP 1.6J defines no such code, so a 1.6
				// peer's unknown frame is deliberately dropped silently (ocpp-go
				// answers it even on 1.6; we do not, because the code is not in the
				// 1.6 error table).
				var ute *ocppj.UnknownMessageTypeError
				if c.version != ocppj.V16 && errors.As(err, &ute) && ute.MsgID != "" {
					c.trySendCallError(ute.MsgID, ocppj.NewCallError(
						ocppj.ErrorCodeMessageTypeNotSupported, ute.Error(), nil))
				}
				continue
			}
			c.cfg.Logger.WarnContext(c.ctx, "ocpp parse error",
				"cp_id", c.id, "err", err)
			// A CALL with a readable message id but a bad shape is answered so the
			// peer does not wait out its own timeout and retry.
			var perr *ocppj.ProtocolError
			if errors.As(err, &perr) && perr.MessageType == int(ocppj.Call) && perr.MsgID != "" {
				// WireCode spells it per version (FormationViolation / FormatViolation).
				c.trySendCallError(perr.MsgID, ocppj.NewCallError(ocppj.ErrorCodeFormationViolation, perr.Message, nil))
			}
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
			// Not cancelled with the connection, for the same reason as the reader.
			wctx, cancel := context.WithTimeout(context.WithoutCancel(c.ctx), c.cfg.WriteTimeout)
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
	if c.inboundGate != nil {
		select {
		case <-c.inboundGate:
		case <-c.ctx.Done():
			return
		}
	}
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
