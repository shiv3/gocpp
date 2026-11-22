// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. See NOTICE.

package ocppj

// The runtime behind ocppj.Server, built on gocpp's per-connection
// dispatcher instead of ocpp-go's DefaultServerDispatcher (one pump goroutine
// and one mutex for every client).
//
// Every ws.Channel gets its own gocpp dispatcher.Conn. The Conn reads frames
// from a bridge fed by the ws.WsServer message handler and writes through
// ws.WsServer.Write, so it works with any ws.WsServer implementation,
// including the mocks of ocpp-go's own test suites.
//
// What stays ocpp-go: request/response structs, profile lookup, validation and
// the CALLERROR code + description for a payload that fails it (Endpoint.ParseMessage
// is called unchanged). What becomes gocpp: frame parsing, message id policy,
// handler goroutines with panic recovery, the FIFO slot for outbound CALLs,
// call timeouts and teardown.

import (
	"gopkg.in/go-playground/validator.v9"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"time"

	gdisp "github.com/shiv3/gocpp/core/dispatcher"
	gocppj "github.com/shiv3/gocpp/core/ocppj"
	gtransport "github.com/shiv3/gocpp/core/transport"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
	"github.com/lorenzodonini/ocpp-go/ocpp"
	"github.com/lorenzodonini/ocpp-go/ws"
)

type gocppRuntime struct {
	s          *Server
	reg        *gdisp.HandlerRegistry
	cfg        gdisp.Config
	started    atomic.Bool
	ctx        context.Context
	cancel     context.CancelFunc
	conns      sync.Map // client id -> *srvConn
	onCanceled CanceledRequestHandler
	// native is the compat ws.Server when the server runs on it: the dispatcher
	// then owns the sockets. nil when the application passed another WsServer, in
	// which case a bridge feeds the frames of its message handler to the dispatcher.
	native      compatrt.Native
	callTimeout time.Duration // set through the compat package; 0 = default
}

type srvConn struct {
	ch       ws.Channel
	br       *bridgeWS
	dc       *gdisp.Conn
	mu       sync.Mutex
	inflight map[string]chan inboundReply // inbound CALLs whose handler has not answered yet
	sent     map[string]struct{}          // outbound CALLs owned by the gocpp dispatcher
}

type inboundReply struct {
	payload []byte
	err     error
}

func newGocppRuntime(s *Server) *gocppRuntime {
	rt := &gocppRuntime{s: s, reg: gdisp.NewHandlerRegistry()}
	if srv, ok := s.server.(*ws.Server); ok {
		if v, ok := compatrt.Get(srv, compatrt.KindNative); ok {
			rt.native, _ = v.(compatrt.Native)
		}
	}
	rt.buildConfig()
	rt.reg.SetFallback(rt.handleCall)
	compatrt.MarkInline(s)
	compatrt.Set(s, compatrt.KindRuntime, rt)
	return rt
}

// buildConfig derives the dispatcher configuration. It runs again at Start, so
// that settings changed after NewServer (SetTimeoutConfig, SetTimeout) count.
func (rt *gocppRuntime) buildConfig() {
	s := rt.s
	cfg := gdisp.DefaultConfig()
	// ocpp-go's server never pings; the idle timeout is the ws layer's PingWait
	// (when the dispatcher owns the socket) and any inbound frame, ping or pong
	// resets it.
	cfg.PingInterval, cfg.PongWait, cfg.ReadTimeout = 0, 0, 0
	if rt.native != nil {
		set := rt.native.Settings()
		cfg.ReadTimeout = set.PingWait
		if set.WriteWait > 0 {
			cfg.WriteTimeout = set.WriteWait
		}
		if set.MaxMessageSize > 0 {
			cfg.MaxMessageSize = set.MaxMessageSize
		}
	}
	// ocpp-go has no handler deadline.
	cfg.HandlerTimeout = 0
	cfg.CallTimeout = defaultMessageTimeout
	if d, ok := s.dispatcher.(*DefaultServerDispatcher); ok && d.timeout > 0 {
		cfg.CallTimeout = d.timeout
	}
	if rt.callTimeout > 0 {
		cfg.CallTimeout = rt.callTimeout
	}
	// Frame-level behaviour of ocpp-go (the same settings as csms.WithOCPPGoCompat).
	cfg.ErrorCodeMapper = gdisp.OCPPGoErrorCodeMapper()
	cfg.MessageIDPolicy = gdisp.MessageIDDrop
	rt.cfg = cfg
}

func (rt *gocppRuntime) running() bool { return rt.started.Load() }

func (rt *gocppRuntime) start() {
	rt.buildConfig()
	rt.ctx, rt.cancel = context.WithCancel(context.Background())
	rt.started.Store(true)
	if rt.native != nil {
		rt.native.SetRuntime(rt)
	}
}

func (rt *gocppRuntime) stop() {
	if !rt.started.Swap(false) {
		return
	}
	if rt.native != nil {
		rt.native.SetRuntime(nil)
	}
	rt.cancel()
	rt.conns.Range(func(k, v any) bool {
		rt.conns.Delete(k)
		sc := v.(*srvConn)
		if sc.br != nil {
			sc.br.shutdown()
		}
		go func() { _ = sc.dc.Close(nil) }()
		return true
	})
}

// Open serves a connection accepted by the compat ws.Server: the dispatcher
// reads and writes the socket itself. Inbound processing is on hold until the
// ws.Server has told the application about the connection (ReleaseInbound).
func (rt *gocppRuntime) Open(c compatrt.Conn) *gdisp.Conn {
	ch, _ := c.Channel.(ws.Channel)
	sc := &srvConn{ch: ch, inflight: map[string]chan inboundReply{}, sent: map[string]struct{}{}}
	meta := gdisp.ConnMetadata{RemoteAddr: c.RemoteAddr, TLS: c.TLS}
	sc.dc = gdisp.NewConnShared(ch.ID(), c.WS, &rt.cfg, rt.reg, meta)
	sc.dc.SetAttachment(sc)
	sc.dc.HoldInbound()
	ctx := rt.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Start before the connection becomes visible to SendRequest.
	sc.dc.Start(ctx)
	rt.register(sc)
	return sc.dc
}

// attach serves a connection of a foreign ws.WsServer through a bridge.
func (rt *gocppRuntime) attach(ch ws.Channel) {
	sc := &srvConn{ch: ch, inflight: map[string]chan inboundReply{}, sent: map[string]struct{}{}}
	sc.br = newBridgeWS(ch.ID(), rt.s.server, rt.subprotocol())
	sc.dc = gdisp.NewConnShared(ch.ID(), sc.br, &rt.cfg, rt.reg)
	sc.dc.SetAttachment(sc)
	ctx := rt.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Start before the connection becomes visible to SendRequest.
	sc.dc.Start(ctx)
	rt.register(sc)
}

func (rt *gocppRuntime) register(sc *srvConn) {
	if old, loaded := rt.conns.Swap(sc.ch.ID(), sc); loaded {
		o := old.(*srvConn)
		if o.br != nil {
			o.br.shutdown()
		}
		go func() { _ = o.dc.Close(nil) }()
	}
}

func (rt *gocppRuntime) detach(ch ws.Channel) {
	v, ok := rt.conns.Load(ch.ID())
	if !ok {
		return
	}
	sc := v.(*srvConn)
	if sc.ch != ch {
		return // already replaced by a newer connection of the same id
	}
	rt.conns.CompareAndDelete(ch.ID(), v)
	if sc.br != nil {
		sc.br.shutdown()
	}
	go func() { _ = sc.dc.Close(nil) }()
}

// subprotocol tells gocpp which OCPP version the connection speaks. A foreign
// ws.WsServer does not expose the negotiated subprotocol through ws.Channel, so
// it is derived from the registered profiles.
func (rt *gocppRuntime) subprotocol() string {
	if _, ok := rt.s.GetProfile("core"); ok {
		return "ocpp1.6"
	}
	return "ocpp2.0.1"
}

// onMessage is the ws.WsServer message handler: it only hands the frame to the
// connection's dispatcher. Unlike ocpp-go it does not report a parse error to
// the ws layer (which only logged it).
func (rt *gocppRuntime) onMessage(ch ws.Channel, data []byte) error {
	v, ok := rt.conns.Load(ch.ID())
	if !ok {
		return fmt.Errorf("no connection state for client %s", ch.ID())
	}
	log.Debugf("received JSON message from %s: %s", ch.ID(), string(data))
	sc := v.(*srvConn)
	// An answer to a request this runtime did not send, but which the application
	// registered by hand in the exported RequestState (ocpp-go's own tests do):
	// take ocpp-go's original path for it.
	if id, ok := answerID(data); ok {
		sc.mu.Lock()
		_, ours := sc.sent[id]
		sc.mu.Unlock()
		if !ours {
			if _, pending := rt.s.RequestState.GetClientState(ch.ID()).GetPendingRequest(id); pending {
				return rt.s.ocppMessageHandler(ch, data)
			}
		}
	}
	return sc.br.push(data)
}

func ocppErrToRaw(e *ocpp.Error) *gdisp.RawCallError {
	return &gdisp.RawCallError{
		Err:  gocppj.NewCallError(gocppj.ErrorCode(e.Code), e.Description, nil),
		Wire: string(e.Code),
	}
}

// handleCall runs on a gocpp handler goroutine (one per inbound CALL, bounded
// per connection, panics recovered).
func (rt *gocppRuntime) handleCall(ctx context.Context, dc *gdisp.Conn, action string, payload []byte) ([]byte, error) {
	s := rt.s
	sc, _ := dc.Attachment().(*srvConn)
	if sc == nil {
		return nil, &gdisp.RawCallError{NoReply: true}
	}
	id, _ := gocppj.MessageIDFromContext(ctx)
	var raw interface{}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return nil, ocppErrToRaw(ocpp.NewError(FormationViolation, err.Error(), id))
	}
	// ocpp-go's own parser decides whether the request is valid and which error answers it.
	fields := []interface{}{float64(CALL), id, action, raw}
	msg, err := s.ParseMessage(fields, nil)
	if err != nil {
		ocppErr := err.(*ocpp.Error)
		if s.invalidMessageHook != nil {
			if err2 := s.invalidMessageHook(sc.ch, ocppErr, string(mustFrame(fields)), fields); err2 != nil {
				ocppErr = err2
				ocppErr.MessageId = id
			}
		}
		log.Error(ocppErr)
		return nil, ocppErrToRaw(ocppErr)
	}
	call := msg.(*Call)
	if s.requestHandler == nil {
		return nil, &gdisp.RawCallError{NoReply: true}
	}
	replyCh := make(chan inboundReply, 1)
	sc.mu.Lock()
	sc.inflight[id] = replyCh
	sc.mu.Unlock()
	defer func() {
		sc.mu.Lock()
		delete(sc.inflight, id)
		sc.mu.Unlock()
	}()
	log.Debugf("handling incoming CALL [%s, %s] from %s", call.UniqueId, call.Action, sc.ch.ID())
	s.requestHandler(sc.ch, call.Payload, call.UniqueId, call.Action)
	// The application answers through SendResponse / SendError, possibly from another goroutine.
	select {
	case r := <-replyCh:
		return r.payload, r.err
	case <-ctx.Done():
		return nil, &gdisp.RawCallError{NoReply: true}
	case <-dc.Context().Done():
		return nil, &gdisp.RawCallError{NoReply: true}
	}
}

func mustFrame(fields []interface{}) []byte {
	b, _ := jsonMarshal(fields)
	return b
}

// reply delivers the answer of an inbound CALL to the gocpp handler waiting for
// it. handled is false when no handler waits for requestID (an answer sent
// outside a request, which ocpp-go also allows): the caller then writes the
// frame itself.
func (rt *gocppRuntime) reply(clientID, requestID string, res *CallResult, cerr *CallError) (handled bool, err error) {
	if rt == nil {
		return false, nil
	}
	v, ok := rt.conns.Load(clientID)
	if !ok {
		return false, nil
	}
	sc := v.(*srvConn)
	sc.mu.Lock()
	ch, ok := sc.inflight[requestID]
	if ok {
		delete(sc.inflight, requestID)
	}
	sc.mu.Unlock()
	if !ok {
		return false, nil
	}
	if cerr != nil {
		ce := gocppj.NewCallError(gocppj.ErrorCode(cerr.ErrorCode), cerr.ErrorDescription, nil)
		if cerr.ErrorDetails != nil {
			// ocpp-go sends whatever the application passed as details, as JSON.
			if raw, err := jsonMarshal(cerr.ErrorDetails); err == nil {
				ce.RawDetails = raw
			}
		}
		rawErr := &gdisp.RawCallError{Err: ce, Wire: string(cerr.ErrorCode)}
		ch <- inboundReply{err: rawErr}
		log.Debugf("sent CALL ERROR [%s] for %s", requestID, clientID)
		return true, nil
	}
	payload, merr := jsonMarshal(res.Payload)
	if merr != nil {
		// Put the entry back so that the caller's fallback error can still be delivered.
		sc.mu.Lock()
		sc.inflight[requestID] = ch
		sc.mu.Unlock()
		return true, merr
	}
	ch <- inboundReply{payload: payload}
	log.Debugf("sent CALL RESULT [%s] for %s", requestID, clientID)
	return true, nil
}

// sendRequest queues an outbound CALL on the connection's own FIFO slot.
func (rt *gocppRuntime) sendRequest(clientID string, call *Call) error {
	v, ok := rt.conns.Load(clientID)
	if !ok {
		return fmt.Errorf("cannot send request %s, no client %s exists", call.UniqueId, clientID)
	}
	sc := v.(*srvConn)
	s := rt.s
	payload, err := jsonMarshal(call.Payload)
	if err != nil {
		return err
	}
	id, action, request := call.UniqueId, call.Action, call.Payload
	s.RequestState.AddPendingRequest(clientID, id, request)
	sc.mu.Lock()
	sc.sent[id] = struct{}{}
	sc.mu.Unlock()
	ctx := gocppj.ContextWithOutboundMessageID(context.Background(), id)
	err = gdisp.DoCallAsync(ctx, sc.dc, action, payload, func(resp []byte, cerr error) {
		s.RequestState.DeletePendingRequest(clientID, id)
		sc.mu.Lock()
		delete(sc.sent, id)
		sc.mu.Unlock()
		rt.complete(sc, id, action, request, resp, cerr)
	})
	if err != nil {
		s.RequestState.DeletePendingRequest(clientID, id)
		sc.mu.Lock()
		delete(sc.sent, id)
		sc.mu.Unlock()
	}
	return err
}

func (rt *gocppRuntime) complete(sc *srvConn, id, action string, request ocpp.Request, resp []byte, cerr error) {
	s := rt.s
	clientID := sc.ch.ID()
	cancel := func(e *ocpp.Error) {
		if rt.onCanceled != nil {
			rt.onCanceled(clientID, id, request, e)
		}
	}
	var ce *gocppj.CallError
	switch {
	case cerr == nil:
		var raw interface{}
		if err := json.Unmarshal(resp, &raw); err != nil {
			cancel(ocpp.NewError(FormationViolation, err.Error(), id))
			return
		}
		profile, _ := s.GetProfileForFeature(action)
		confirmation, err := profile.ParseResponse(action, raw, parseRawJsonConfirmation)
		if err != nil {
			cancel(ocpp.NewError(FormationViolation, err.Error(), id))
			return
		}
		callResult := CallResult{MessageTypeId: CALL_RESULT, UniqueId: id, Payload: confirmation}
		if err = Validate.Struct(callResult); err != nil {
			cancel(errorFromValidation(err.(validator.ValidationErrors), id, action))
			return
		}
		log.Debugf("handling incoming CALL RESULT [%s] from %s", id, clientID)
		if s.responseHandler != nil {
			s.responseHandler(sc.ch, confirmation, id)
		}
	case errors.As(cerr, &ce):
		log.Debugf("handling incoming CALL ERROR [%s] from %s", id, clientID)
		// ocpp-go's own rules for an error message: the details are whatever JSON
		// the peer sent, and the message is validated like any other.
		var details interface{}
		if len(ce.RawDetails) > 0 {
			var d interface{}
			if err := json.Unmarshal(ce.RawDetails, &d); err == nil {
				details = d
			}
		}
		callError := CallError{
			MessageTypeId:    CALL_ERROR,
			UniqueId:         id,
			ErrorCode:        ocpp.ErrorCode(ce.Code),
			ErrorDescription: ce.Description,
			ErrorDetails:     details,
		}
		if err := Validate.Struct(callError); err != nil {
			cancel(errorFromValidation(err.(validator.ValidationErrors), id, ""))
			return
		}
		if s.errorHandler != nil {
			s.errorHandler(sc.ch, ocpp.NewError(callError.ErrorCode, callError.ErrorDescription, id), callError.ErrorDetails)
		}
	case errors.Is(cerr, gocppj.ErrCallTimeout):
		log.Infof("request %v for %v timed out", id, clientID)
		cancel(ocpp.NewError(GenericError, "Request timed out", id))
	case sc.dc.Context().Err() != nil:
		// The connection is gone. ocpp-go does not report requests that were still
		// pending as canceled in that case: the 1.6 / 2.0.1 glue answers the
		// application from its disconnect handler, so reporting here too would
		// deliver the callback twice (or leave a stray error on Errors()).
	default:
		// Queue wait cancelled, write failure.
		cancel(ocpp.NewError(InternalError, cerr.Error(), id))
	}
}

// bridgeWS adapts one client of a ws.WsServer to gocpp's transport.WS.
type bridgeWS struct {
	id   string
	srv  ws.WsServer
	sub  string
	in   chan []byte
	done chan struct{}
	once sync.Once
}

func newBridgeWS(id string, srv ws.WsServer, sub string) *bridgeWS {
	return &bridgeWS{id: id, srv: srv, sub: sub, in: make(chan []byte, 8), done: make(chan struct{})}
}

func (b *bridgeWS) push(data []byte) error {
	select {
	case b.in <- data:
		return nil
	case <-b.done:
		return errors.New("connection closed")
	}
}

func (b *bridgeWS) shutdown() { b.once.Do(func() { close(b.done) }) }

func (b *bridgeWS) Read(ctx context.Context) ([]byte, error) {
	select {
	case m := <-b.in:
		return m, nil
	case <-b.done:
		return nil, errors.New("connection closed")
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (b *bridgeWS) Write(_ context.Context, data []byte) error {
	select {
	case <-b.done:
		return errors.New("connection closed")
	default:
	}
	return b.srv.Write(b.id, data)
}

func (b *bridgeWS) Ping(context.Context) error { return nil }

func (b *bridgeWS) Close(gtransport.StatusCode, string) error {
	b.shutdown()
	return nil
}

func (b *bridgeWS) Subprotocol() string { return b.sub }

// answerID returns the message id of a CALLRESULT or CALLERROR frame.
func answerID(data []byte) (string, bool) {
	i := 0
	for i < len(data) && (data[i] == ' ' || data[i] == '\t' || data[i] == '\n' || data[i] == '\r') {
		i++
	}
	if i+1 >= len(data) || data[i] != '[' {
		return "", false
	}
	var head []json.RawMessage
	if data[i+1] != '3' && data[i+1] != '4' {
		return "", false
	}
	if err := json.Unmarshal(data, &head); err != nil || len(head) < 2 {
		return "", false
	}
	var id string
	if err := json.Unmarshal(head[1], &id); err != nil {
		return "", false
	}
	return id, true
}

// useGocppRuntime decides which runtime serves an ocppj.Server.
//
//   - no dispatcher passed (ocppj.NewServer(ws, nil, nil), ocpp16.NewCentralSystem(nil, ws)): gocpp.
//   - a dispatcher passed by the application: ocpp-go's original code, because the
//     application may drive and inspect that dispatcher itself.
//
// OCPPGO_COMPAT_RUNTIME=gocpp also selects gocpp for an injected
// *DefaultServerDispatcher (CI runs ocpp-go's suites that way); =legacy forces
// the original code everywhere.
func useGocppRuntime(injected ServerDispatcher) bool {
	switch os.Getenv("OCPPGO_COMPAT_RUNTIME") {
	case "legacy":
		return false
	case "gocpp":
		_, isDefault := injected.(*DefaultServerDispatcher)
		return injected == nil || isDefault
	}
	return injected == nil
}

// SetCallTimeout sets the timeout of outbound calls (see compat.SetCallTimeout).
func (rt *gocppRuntime) SetCallTimeout(d time.Duration) { rt.callTimeout = d }
