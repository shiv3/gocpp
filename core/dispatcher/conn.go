package dispatcher

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
)

// Conn is one OCPP connection. It owns one resident reader goroutine whose
// lifetime is bound to ctx; writes happen on the sender's goroutine under a
// per-connection FIFO write lock (there is no writer goroutine or queue). All
// teardown flows from cancel(cause).
type Conn struct {
	id      string
	ws      transport.WS
	version ocppj.Version
	meta    ConnMetadata

	// calls counts outbound calls that have started and not yet returned
	// (queued for the slot, awaiting a reply). drainBit is set once the
	// connection is draining: no call may start any more. Both live in one word
	// so that "idle and mark draining" is a single compare-and-swap.
	calls atomic.Int64

	// attachment is owner-defined data hung on the connection (see SetAttachment).
	attachment atomic.Value
	// attMu serialises read-modify-write updates of the attachment
	// (UpdateAttachment).
	attMu sync.Mutex
	// inboundGate, when set, filters inbound CALL/SEND before they are scheduled.
	inboundGate InboundGate
	// inboundDone, when set, is called once for every frame the gate admitted, after
	// its handler returned.
	inboundDone func(f ocppj.Frame)

	// wlock is the write lock: a channel of capacity 1 so that waiting for it can
	// honour a context and blocked senders are served FIFO. A sender holds it
	// across ws.Write and the frame-observer call (wire order). wwait counts
	// senders waiting for or holding it (the OutboundQueueDepth metric);
	// errReplies bounds the reader's in-flight protocol-error replies. See send.go.
	wlock      chan struct{}
	wwait      atomic.Int32
	errReplies atomic.Int32

	// Write watchdog (see writeWatchdog in send.go); used only when ws is a
	// transport.Aborter. wStart is the mono offset (+1) at which the write in
	// flight began, 0 between writes; wArmed says wTimer is running.
	aborter transport.Aborter
	wStart  atomic.Int64
	wArmed  atomic.Bool
	wTimer  atomic.Pointer[time.Timer]

	pending *pendingStore
	reg     *HandlerRegistry
	slot    callSlot

	ctx    context.Context
	cancel context.CancelCauseFunc
	// noCancel is c.ctx without its cancellation (values only); reads and writes
	// use it so cancelling the connection never aborts a socket operation without
	// a close frame. handlerBase is noCancel plus the *Conn value handler
	// contexts are built on.
	noCancel     context.Context
	handlerBase  context.Context
	closeWSOnce  sync.Once
	closedMetric sync.Once
	done         chan struct{}
	wg           sync.WaitGroup

	// Transient background goroutines (async calls and their callbacks, the
	// OCPP keepalive loop, ping callbacks). bgWG tracks them; bgClosed (under
	// bgMu) gates new ones so Add can never race Close's Wait. See goBG/enterBG.
	bgMu     sync.Mutex
	bgClosed bool
	bgWG     sync.WaitGroup

	// Inbound CALL/SEND scheduling, see inbound.go.
	inMu        sync.Mutex
	inQ         []ocppj.Frame
	running     int
	gateOpen    bool
	inSpace     chan struct{}
	maxHandlers int

	// Activity timestamps (UnixNano) behind Stats.
	connectedAt atomic.Int64
	lastRecv    atomic.Int64
	lastSent    atomic.Int64

	// Keepalive timers (see keepalive.go). recvMono is the last inbound activity
	// as a monotonic offset from monoBase, so the read idle watchdog is immune to
	// wall-clock steps (lastRecv is wall time, for Stats only).
	monoBase time.Time
	recvMono atomic.Int64
	tmu      sync.Mutex // guards the fields below
	// timersStopped is set at teardown; no timer callback may start after it.
	timersStopped bool
	wdTimer       *time.Timer
	pingTimer     *time.Timer

	cfg *Config
	// pipe is the payload policy (validation, signing, error mapping) for this
	// connection; see Pipeline.
	pipe Pipeline
}

// ConnMetadata contains HTTP upgrade metadata associated with a connection.
// Fields are empty for connections that were not created from an inbound HTTP
// upgrade request.
type ConnMetadata struct {
	// Owner is opaque data of whoever creates the connection (the CSMS server
	// keeps its gateway session here), returned by Conn.Owner. It is not cloned.
	//
	// Experimental: this API may change before v1.0.
	Owner         any
	RemoteAddr    string
	RequestHeader http.Header
	TLS           *tls.ConnectionState
}

// NewConn creates a connection. Call Start to launch its goroutines.
func NewConn(id string, ws transport.WS, cfg Config, reg *HandlerRegistry, meta ...ConnMetadata) *Conn {
	return NewConnShared(id, ws, &cfg, reg, meta...)
}

// NewConnShared is NewConn for a Config shared by many connections (a server
// builds one per negotiated version instead of copying it for every
// connection). The Config is read concurrently by every connection that uses it
// and must not be modified after this call.
func NewConnShared(id string, ws transport.WS, cfg *Config, reg *HandlerRegistry, meta ...ConnMetadata) *Conn {
	m := ConnMetadata{}
	if len(meta) > 0 {
		m = cloneConnMetadata(meta[0])
	}
	c := &Conn{
		id:          id,
		ws:          ws,
		version:     subprotocolToVersion(ws.Subprotocol()),
		meta:        m,
		wlock:       make(chan struct{}, 1),
		pending:     newPendingStore(),
		reg:         reg,
		gateOpen:    true,
		maxHandlers: int(max(cfg.MaxConcurrentHandlers, 1)),
		done:        make(chan struct{}),
		cfg:         cfg,
	}
	c.pipe = NewPipeline(cfg, c.version, id)
	return c
}

// HoldInbound makes the connection withhold inbound CALL and SEND handling until
// ReleaseInbound is called. Call it before Start. The server side uses it so no
// handler runs before the connection is registered and its connect callback has
// returned. Connections that never call it dispatch immediately. Held frames wait
// in the inbound queue (InboundQueueSize); the reader keeps resolving
// CALLRESULTs until that queue is full.
func (c *Conn) HoldInbound() {
	c.inMu.Lock()
	c.gateOpen = false
	c.inMu.Unlock()
}

// ReleaseInbound opens the gate armed by HoldInbound and starts handling frames
// that arrived meanwhile. Idempotent; a no-op when the gate was never armed.
func (c *Conn) ReleaseInbound() {
	c.inMu.Lock()
	defer c.inMu.Unlock()
	if c.gateOpen {
		return
	}
	c.gateOpen = true
	for len(c.inQ) > 0 && c.running < c.maxHandlers && c.ctx.Err() == nil {
		c.running++
		go c.handlerLoop(c.popLocked())
	}
	c.wakeReaderLocked()
}

// ID returns the charge point identifier.
func (c *Conn) ID() string { return c.id }

// Version returns the negotiated OCPP version.
func (c *Conn) Version() ocppj.Version { return c.version }

// Subprotocol returns the negotiated WebSocket subprotocol.
func (c *Conn) Subprotocol() string { return c.ws.Subprotocol() }

// Owner returns the ConnMetadata.Owner the connection was created with.
//
// Experimental: this API may change before v1.0.
func (c *Conn) Owner() any { return c.meta.Owner }

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

// ConnStats is a snapshot of a connection's activity. Zero times mean the event
// has not happened (LastSentAt before the first write).
type ConnStats struct {
	ConnectedAt time.Time
	// LastReceivedAt is the last inbound activity of any kind: a data frame, a
	// WebSocket ping or a pong. It starts at ConnectedAt.
	LastReceivedAt time.Time
	// LastSentAt is the last successful outbound write: an OCPP frame or a
	// WebSocket ping.
	LastSentAt time.Time
	// PendingCalls is the number of outbound CALLs awaiting a response.
	PendingCalls int
	// QueuedCalls is the number of outbound CALLs waiting for the connection's
	// single outstanding-call slot (always 0 when serialization is off).
	QueuedCalls int
}

// Stats returns a snapshot of the connection's activity counters.
func (c *Conn) Stats() ConnStats {
	ts := func(v int64) time.Time {
		if v == 0 {
			return time.Time{}
		}
		return time.Unix(0, v)
	}
	pending := c.pending.len()
	c.slot.mu.Lock()
	queued := len(c.slot.q)
	c.slot.mu.Unlock()
	return ConnStats{
		ConnectedAt:    ts(c.connectedAt.Load()),
		LastReceivedAt: ts(c.lastRecv.Load()),
		LastSentAt:     ts(c.lastSent.Load()),
		PendingCalls:   pending,
		QueuedCalls:    queued,
	}
}

// NoteActivity records inbound activity, resetting the read idle watchdog.
// It is safe to call from the websocket read goroutine (e.g. ping/pong
// callbacks): it only stores two timestamps and never blocks.
func (c *Conn) NoteActivity() {
	c.lastRecv.Store(time.Now().UnixNano())
	c.recvMono.Store(int64(time.Since(c.monoBase)))
}

func cloneConnMetadata(meta ConnMetadata) ConnMetadata {
	cloned := ConnMetadata{
		Owner:         meta.Owner,
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

// drainingCloseReason is the WebSocket close reason of a connection closed by a
// drain (close code 1012, service restart).
const drainingCloseReason = "gateway draining"

// closedByServerReason is the WebSocket close reason sent when the server
// closes the connection on purpose.
const closedByServerReason = "closed by the server"

// closeWS closes the WebSocket with a close frame. The first call decides the
// code; later calls are no-ops.
func (c *Conn) closeWS(code transport.StatusCode, reason string) {
	c.closeWSOnce.Do(func() { _ = c.ws.Close(code, reason) })
}

// closeForCause closes the socket gracefully with the close code that matches
// why the connection ended. The reader and writer deliberately do not abort the
// socket on cancellation (the websocket library drops the TCP connection without
// a close frame when an in-flight operation's ctx is cancelled), so teardown is
// what tells the peer why.
func (c *Conn) closeForCause() {
	cause := context.Cause(c.ctx)
	var status *CloseStatus
	switch {
	case errors.As(cause, &status):
		c.closeWS(status.Code, truncateCloseReason(status.Reason))
	case errors.Is(cause, ocppj.ErrGatewayDraining), errors.Is(cause, ocppj.ErrDrainTimeout):
		c.closeWS(transport.StatusServiceRestart, drainingCloseReason)

	case errors.Is(cause, ocppj.ErrConnReplaced):
		c.closeWS(transport.StatusPolicyViolation, replacedCloseReason)
	case errors.Is(cause, ocppj.ErrConnClosedByServer):
		c.closeWS(transport.StatusNormalClosure, closedByServerReason)
	default:
		c.closeWS(transport.StatusNormalClosure, "closed")
	}
}

// CloseStatus is a close cause that chooses the WebSocket close code and reason
// the peer is sent. Pass it to Conn.Abort (see also Conn.AbortWithStatus). It
// is an ocppj.ErrConnClosedByServer for errors.Is, so the connection counts as
// closed by the server, only with a code of the caller's choosing.
type CloseStatus struct {
	Code   transport.StatusCode
	Reason string
}

func (e *CloseStatus) Error() string {
	return fmt.Sprintf("ocpp: connection closed by the server (status %d %q)", int(e.Code), e.Reason)
}

// Unwrap makes errors.Is(e, ocppj.ErrConnClosedByServer) true.
func (e *CloseStatus) Unwrap() error { return ocppj.ErrConnClosedByServer }

// AbortWithStatus is Abort with a CloseStatus cause: the socket is closed with
// a close frame carrying code and reason. If the connection was already
// cancelled for another cause that one stands, as with Abort.
func (c *Conn) AbortWithStatus(code transport.StatusCode, reason string) {
	c.Abort(&CloseStatus{Code: code, Reason: reason})
}

// maxCloseReason is the longest close frame reason RFC 6455 allows (125 bytes
// of control frame payload minus the 2 byte code).
const maxCloseReason = 123

func truncateCloseReason(r string) string {
	if len(r) <= maxCloseReason {
		return r
	}
	r = r[:maxCloseReason]
	for len(r) > 0 && !utf8.ValidString(r) {
		r = r[:len(r)-1]
	}
	return r
}

// teardown runs exactly once, on its own goroutine, when the connection context
// is done (context.AfterFunc). It replaces a closer goroutine and a goroutine
// that waited for the others, so a healthy connection holds only its reader,
// writer.
//
// Order: close the socket first (that is what unblocks the reader and tells the
// peer why), then fail queued async calls (user callbacks run here, and a slow
// one must not delay the close frame), then wait for the connection goroutines.
// It takes no lock while waiting, and done closes only after the reader has
// returned.
func (c *Conn) teardown() {
	c.stopTimers()
	c.closeForCause()
	// Take the write lock for good: waits for a write that is in flight (closing
	// the socket has unblocked it, or WriteTimeout ends it) and guarantees no
	// write or frame-observer call after done.
	c.wlock <- struct{}{}
	c.failQueued()
	c.dropInbound()
	c.wg.Wait()
	close(c.done)
}

// enterBG registers a transient background goroutine (or timer callback) of the
// connection. It returns false once Close has begun draining, in which case the
// caller must not start. Every true must be paired with leaveBG.
func (c *Conn) enterBG() bool {
	c.bgMu.Lock()
	defer c.bgMu.Unlock()
	if c.bgClosed {
		return false
	}
	c.bgWG.Add(1)
	return true
}

func (c *Conn) leaveBG() { c.bgWG.Done() }

// goBG runs f on a new goroutine tracked by Close. It reports false, without
// starting f, when the connection is already draining.
func (c *Conn) goBG(f func()) bool {
	if !c.enterBG() {
		return false
	}
	go func() {
		defer c.leaveBG()
		f()
	}()
	return true
}

// Start launches the connection goroutines bound to parent.
func (c *Conn) Start(parent context.Context) {
	c.ctx, c.cancel = context.WithCancelCause(parent)
	now := time.Now().UnixNano()
	c.aborter, _ = c.ws.(transport.Aborter)
	c.noCancel = context.WithoutCancel(c.ctx)
	c.handlerBase = context.WithValue(c.noCancel, connKey{}, c)
	c.connectedAt.Store(now)
	c.lastRecv.Store(now)
	c.monoBase = time.Now()
	c.cfg.Metrics.ConnectionOpened()

	c.wg.Add(1)
	go c.reader()
	c.startWebSocketPing()
	c.startReadWatchdog()

	// Registered last, after every wg.Add and timer exists: AfterFunc also fires
	// immediately when parent is already cancelled.
	context.AfterFunc(c.ctx, c.teardown)
}

// SetAttachment stores owner data on the connection so that code holding only
// the *Conn (a handler reaching it through ConnFromContext) can find it. The
// stored values must all have the same concrete type. The CSMS server keeps the
// connection's registry session here.
//
// Experimental: this API may change before v1.0.
func (c *Conn) SetAttachment(v any) {
	c.attMu.Lock()
	c.attachment.Store(v)
	c.attMu.Unlock()
}

// UpdateAttachment replaces the attachment with f(current) atomically with
// respect to other UpdateAttachment and SetAttachment calls, so two owners that
// each derive a new value from the old one (a session's attributes and its
// epoch, say) cannot overwrite each other's change. f runs under a lock and must
// not block or call back into the connection. Like SetAttachment, the result
// must have the same concrete type as every earlier value.
//
// Experimental: this API may change before v1.0.
func (c *Conn) UpdateAttachment(f func(old any) any) {
	c.attMu.Lock()
	c.attachment.Store(f(c.attachment.Load()))
	c.attMu.Unlock()
}

// Attachment returns the value stored by SetAttachment, or nil.
func (c *Conn) Attachment() any { return c.attachment.Load() }

// InboundGate decides, for every inbound CALL and SEND, whether it is scheduled
// at all. It returns nil to admit the frame, or the error to refuse it with: a
// refused CALL is answered with that CALLERROR (counted against the same bound
// on queued error replies as other built-in errors), a refused SEND is dropped.
// Nothing about the frame has been validated or decoded when the gate runs, so it
// must be cheap and must not block. It is called on the connection's reader
// goroutine, one frame at a time.
//
// Experimental: this API may change before v1.0.
type InboundGate func(f ocppj.Frame) *ocppj.CallError

// SetInboundGate installs g (nil removes it). Call it before Start: the reader
// reads the field without synchronisation.
//
// Experimental: this API may change before v1.0.
func (c *Conn) SetInboundGate(g InboundGate) { c.inboundGate = g }

// SetInboundDone installs a callback that runs after the handler of every inbound
// CALL and SEND has returned (and its reply was queued), so a gate that counts
// admitted frames, such as a concurrency limit, can release them. Frames the gate
// admitted but that never reach a handler because the connection ended are not
// reported: the owner reconciles its count when it tears the connection down.
// Call it before Start. It runs on the handler goroutine and must not block.
//
// Experimental: this API may change before v1.0.
func (c *Conn) SetInboundDone(f func(ocppj.Frame)) { c.inboundDone = f }

// drainBit marks Conn.calls as draining.
const drainBit = int64(1) << 62

// beginCall registers an outbound call. It fails once the connection is
// draining, before anything was written.
func (c *Conn) beginCall() bool {
	if c.calls.Add(1)&drainBit != 0 {
		c.calls.Add(-1)
		return false
	}
	return true
}

func (c *Conn) endCall() { c.calls.Add(-1) }

// Draining reports whether the connection stopped accepting new outbound calls
// because a drain began (TryDrain or ForceDrain).
//
// Experimental: this API may change before v1.0.
func (c *Conn) Draining() bool { return c.calls.Load()&drainBit != 0 }

// Quiesce stops the connection from starting new outbound calls (they fail with
// ocppj.ErrGatewayDraining before anything is written) without closing it, so
// calls already started can finish. Call TryDrain afterwards to close it once
// it is idle.
//
// Experimental: this API may change before v1.0.
func (c *Conn) Quiesce() { c.calls.Or(drainBit) }

// Unquiesce undoes Quiesce: the connection accepts new outbound calls again. Use
// it when a drain that quiesced the connection ends without closing it. It has no
// effect on a connection TryDrain or ForceDrain has already closed.
//
// Experimental: this API may change before v1.0.
func (c *Conn) Unquiesce() {
	if c.Context().Err() != nil {
		return
	}
	c.calls.And(^drainBit)
}

// TryDrain closes the connection for a drain if it is idle: no inbound handler
// running or queued, no outbound call queued, in flight or awaiting a reply. It
// is atomic with respect to new outbound calls, which from then on fail with
// ocppj.ErrGatewayDraining before anything is written. On success the close
// frame (1012, service restart) is sent in the background and TryDrain reports
// true; a busy connection is left untouched and TryDrain reports false. A frame
// the charge point sends at the same moment may still be lost, as with any
// disconnect.
//
// Experimental: this API may change before v1.0.
func (c *Conn) TryDrain() bool {
	c.inMu.Lock()
	busy := c.running > 0 || len(c.inQ) > 0
	c.inMu.Unlock()
	if busy {
		return false
	}
	c.slot.mu.Lock()
	queued := len(c.slot.q)
	c.slot.mu.Unlock()
	if queued > 0 {
		return false
	}
	// Idle means no call counted besides the drain bit itself; swap to exactly
	// the bit so that no call can slip in between the check and the close.
	for {
		v := c.calls.Load()
		if v&^drainBit != 0 {
			return false
		}
		if c.calls.CompareAndSwap(v, drainBit) {
			break
		}
	}
	c.Abort(ocppj.ErrGatewayDraining)
	return true
}

// ForceDrain closes the connection for a drain whatever it is doing, at the end
// of the drain window. Calls in flight fail with ocppj.ErrDrainTimeout, which
// (unlike ErrGatewayDraining) does not prove they were not executed.
//
// Experimental: this API may change before v1.0.
func (c *Conn) ForceDrain() {
	c.calls.Or(drainBit)
	c.Abort(ocppj.ErrDrainTimeout)
}

// Abort cancels the connection with cause without waiting for teardown. The
// socket is then closed in the background with a close frame matching cause
// (see teardown); call Close afterwards to wait for completion.
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
	// Stop accepting new background work, then wait for in-flight async calls,
	// the keepalive loop and timer callbacks to drain (their DoCalls have already failed via ctx/pending).
	c.bgMu.Lock()
	c.bgClosed = true
	c.bgMu.Unlock()
	c.bgWG.Wait()
	// Close can be called by more than one party (a replacement and the
	// connection's own teardown); the gauge must drop once.
	c.closedMetric.Do(c.cfg.Metrics.ConnectionClosed)
	return nil
}

func (c *Conn) reader() {
	defer c.wg.Done()
	for {
		// Read with a ctx that survives cancellation: cancelling an in-flight read
		// makes the websocket library drop the TCP connection without a close
		// frame. Teardown closes the socket, which ends this read.
		msg, err := c.ws.Read(c.noCancel)
		if err != nil {
			c.cancel(fmt.Errorf("read: %w", err))
			return
		}
		if !c.handleMessage(msg) {
			return
		}
	}
}

// handleMessage processes one inbound data frame: parse, observe, answer
// protocol errors, resolve a pending call or hand a CALL/SEND to the handler
// queue. It returns false when the connection context is done and the reader
// must stop. It never runs a handler and never waits for one except when the
// inbound queue is full (see enqueueInbound). A transport that pushes frames
// instead of blocking in Read can drive the dispatcher by calling it.
func (c *Conn) handleMessage(msg []byte) bool {
	c.NoteActivity()
	frame, err := ocppj.Parse(msg)
	if err == nil && !c.supportsMessageType(frame.Type) {
		// Parse is version-agnostic; types 5 and 6 are OCPP 2.1 only. Treat
		// them as unknown on older links so they neither resolve a pending
		// call nor reach a SEND handler.
		frame, err = ocppj.Frame{}, &ocppj.UnknownMessageTypeError{MessageType: int(frame.Type), MsgID: frame.MsgID}
	}
	c.observeInbound(msg, frame, err)
	if err != nil {
		if errors.Is(err, ocppj.ErrIgnoredMessageType) {
			c.cfg.Logger.DebugContext(c.ctx, "ignored unknown message type",
				"cp_id", c.id, "err", err)
			// OCPP 2.0.1 / 2.1 Part 4 §4.2.3: a message with a MessageTypeId the
			// receiver does not support is answered with CALLERROR
			// MessageTypeNotSupported. OCPP 1.6J defines no such code, so by
			// default a 1.6 peer's unknown frame is dropped silently (ocpp-go
			// answers it even on 1.6; an ErrorCodeMapper can do the same).
			var ute *ocppj.UnknownMessageTypeError
			if errors.As(err, &ute) && ute.MsgID != "" {
				def := ocppj.NewCallError(ocppj.ErrorCodeMessageTypeNotSupported, ute.Error(), nil)
				if wire, reply := c.pipe.MapBuiltinError(ErrorSourceUnknownMessageType, "", ute, def, c.version != ocppj.V16); reply {
					c.trySendCallErrorWire(ute.MsgID, wire, def.Description)
				}
			}
			return true
		}
		c.cfg.Logger.WarnContext(c.ctx, "ocpp parse error",
			"cp_id", c.id, "err", err)
		// A CALL with a readable message id but a bad shape is answered so the
		// peer does not wait out its own timeout and retry.
		var perr *ocppj.ProtocolError
		if errors.As(err, &perr) && perr.MessageType == int(ocppj.Call) && perr.MsgID != "" {
			// WireCode spells it per version (FormationViolation / FormatViolation).
			def := ocppj.NewCallError(ocppj.ErrorCodeFormationViolation, perr.Message, nil)
			if wire, reply := c.pipe.MapBuiltinError(ErrorSourceMalformedFrame, "", perr, def, true); reply {
				c.trySendCallErrorWire(perr.MsgID, wire, def.Description)
			}
		}
		return true
	}
	if !c.checkMessageID(frame) {
		return true
	}
	return c.routeInbound(frame)
}

// supportsMessageType reports whether the negotiated version defines the
// OCPP-J message type. CALLRESULTERROR (5) and SEND (6) exist only in 2.1.
func (c *Conn) supportsMessageType(t ocppj.MessageType) bool {
	switch t {
	case ocppj.MessageTypeCallResultError, ocppj.Send:
		return c.version == ocppj.V21
	}
	return true
}

// routeInbound dispatches a parsed frame. It returns false when the connection
// context is done and the reader must stop.
func (c *Conn) routeInbound(frame ocppj.Frame) bool {
	switch frame.Type {
	case ocppj.CallResult:
		c.pending.resolve(frame.MsgID, rawResult{payload: frame.Payload})
	case ocppj.MessageTypeCallError:
		ce := &ocppj.CallError{Code: ocppj.ErrorCode(frame.ErrCode), Description: frame.ErrDesc, RawDetails: cloneRaw(frame.ErrData)}
		c.pending.resolve(frame.MsgID, rawResult{err: ce})
	case ocppj.MessageTypeCallResultError:
		ce := &ocppj.CallError{Code: ocppj.ErrorCode(frame.ErrCode), Description: frame.ErrDesc, RawDetails: cloneRaw(frame.ErrData), IsResultError: true}
		c.pending.resolve(frame.MsgID, rawResult{err: ce})
	case ocppj.Call, ocppj.Send:
		if g := c.inboundGate; g != nil {
			if ce := g(frame); ce != nil {
				if frame.Type == ocppj.Call && frame.MsgID != "" {
					c.trySendCallErrorWire(frame.MsgID, ce.WireCode(c.version), ce.Description)
				}
				return true
			}
		}
		return c.enqueueInbound(frame)
	}
	return true
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

// checkMessageID applies the message id policy to an inbound CALL. It returns
// false when the frame must be dropped (a CALLERROR has already been queued if
// the policy answers it).
func (c *Conn) checkMessageID(frame ocppj.Frame) bool {
	if frame.Type != ocppj.Call && frame.Type != ocppj.Send {
		return true
	}
	empty := frame.MsgID == ""
	long := utf8.RuneCountInString(frame.MsgID) > ocppj.MaxMessageIDLength
	if !empty && !long {
		return true
	}
	policy := c.cfg.MessageIDPolicy
	if policy == MessageIDAuto {
		switch c.cfg.SchemaMode {
		case SchemaModeStrict:
			policy = MessageIDReject
		default:
			policy = MessageIDAccept
		}
	}
	switch policy {
	case MessageIDAccept:
		if c.cfg.SchemaMode == SchemaModeLenient || c.cfg.SchemaMode == SchemaModeTolerant {
			c.cfg.Logger.WarnContext(c.ctx, "message id breaks the OCPP-J limit; accepted",
				"cp_id", c.id, "action", frame.Action, "empty", empty, "length", utf8.RuneCountInString(frame.MsgID))
		}
		return true
	case MessageIDReject:
		if long && frame.Type == ocppj.Call {
			def := ocppj.NewCallError(ocppj.ErrorCodeFormationViolation,
				fmt.Sprintf("message id longer than %d characters", ocppj.MaxMessageIDLength), nil)
			if wire, reply := c.pipe.MapBuiltinError(ErrorSourceMessageID, frame.Action, nil, def, true); reply {
				c.trySendCallErrorWire(frame.MsgID, wire, def.Description)
			}
		}
	}
	c.cfg.Logger.WarnContext(c.ctx, "dropping message with invalid message id",
		"cp_id", c.id, "action", frame.Action, "empty", empty, "length", utf8.RuneCountInString(frame.MsgID))
	return false
}

// cloneRaw copies b, which aliases the inbound frame buffer. It returns nil for
// an absent element.
func cloneRaw(b []byte) json.RawMessage {
	if len(b) == 0 {
		return nil
	}
	return append(json.RawMessage(nil), b...)
}
