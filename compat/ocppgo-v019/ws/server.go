// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. It implements ws.Server, whose
// exported API is that of ocpp-go v0.17.2-fix.3, on gocpp's transport
// (coder/websocket). See NOTICE.

package ws

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/gorilla/mux"
	gorilla "github.com/gorilla/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	gocppj "github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
)

const (
	// outQueueSize is the size of the output queue of a connection that is not
	// served by the gocpp dispatcher. Generously buffered so that Write's
	// non-blocking send only fails when the connection is genuinely stalled.
	outQueueSize = 32
	// closeHandshakeWait bounds how long a graceful close waits for the peer's
	// close frame.
	closeHandshakeWait = 5 * time.Second
)

// serverConn is the per-connection state of a connection accepted by a Server.
type serverConn struct {
	srv *Server
	c   *websocket.Conn
	t   transport.WS
	// dc is set when the connection is served by the gocpp dispatcher (a runtime
	// was attached, see compatrt.Native). Then there is no read loop, write queue
	// or timer here: the dispatcher owns them.
	dc *dispatcher.Conn

	// The fields below are used by connections that are not served by the
	// dispatcher: the server then runs a read loop and a write queue itself,
	// as ocpp-go does.
	outQueue chan []byte
	closeC   chan websocket.CloseError
	done     chan struct{}
	idle     *time.Timer
	cleanup  sync.Once
}

// Server is the websocket server of ocpp-go, implemented on coder/websocket.
//
// Use the NewServer or NewTLSServer functions to create a new server.
type Server struct {
	connections         map[string]*WebSocket
	httpServer          *http.Server
	tlsConfig           *tls.Config
	messageHandler      func(ws Channel, data []byte) error
	checkClientHandler  func(id string, r *http.Request) bool
	newClientHandler    func(ws Channel)
	disconnectedHandler func(ws Channel)
	basicAuthHandler    func(username string, password string) bool
	tlsCertificatePath  string
	tlsCertificateKey   string
	timeoutConfig       ServerTimeoutConfig
	subprotocols        []string
	checkOrigin         func(r *http.Request) bool
	connMutex           sync.RWMutex
	httpHandler         *mux.Router

	stateMu sync.Mutex // guards errC, addr, httpServer replacement
	errMu   sync.RWMutex
	errC    chan error
	addr    *net.TCPAddr
	// stopped is closed by Stop and unblocks error reports nobody reads.
	stopped  chan struct{}
	stopping atomic.Bool
	rt       atomic.Pointer[runtimeBox]
	adm      admissionState // see admission.go
}

type runtimeBox struct{ rt compatrt.Runtime }

// native adapts a Server to compatrt.Native without adding to its exported API.
type native struct{ s *Server }

func (n native) SetRuntime(rt compatrt.Runtime) {
	if rt == nil {
		n.s.rt.Store(nil)
		return
	}
	n.s.rt.Store(&runtimeBox{rt})
}

func (n native) Settings() compatrt.ServerSettings {
	c := n.s.timeoutConfig
	return compatrt.ServerSettings{WriteWait: c.WriteWait, PingWait: c.PingWait, MaxMessageSize: serverMaxMessageSize(c)}
}

func newServer() *Server {
	s := &Server{
		httpServer:    &http.Server{},
		timeoutConfig: NewServerTimeoutConfig(),
		httpHandler:   mux.NewRouter(),
		stopped:       make(chan struct{}),
	}
	compatrt.Set(s, compatrt.KindNative, native{s})
	return s
}

// Creates a new simple websocket server (the websockets are not secured).
func NewServer() *Server {
	return newServer()
}

// NewTLSServer creates a new secure websocket server. All created websocket channels will use TLS.
//
// You need to pass a filepath to the server TLS certificate and key.
//
// It is recommended to pass a valid TLSConfig for the server to use.
// For example to require client certificate verification:
//
//	tlsConfig := &tls.Config{
//		ClientAuth: tls.RequireAndVerifyClientCert,
//		ClientCAs: clientCAs,
//	}
//
// If no tlsConfig parameter is passed, the server will by default
// not perform any client certificate verification.
func NewTLSServer(certificatePath string, certificateKey string, tlsConfig *tls.Config) *Server {
	s := newServer()
	s.tlsCertificatePath = certificatePath
	s.tlsCertificateKey = certificateKey
	s.tlsConfig = tlsConfig
	s.httpServer = &http.Server{TLSConfig: tlsConfig}
	return s
}

func (server *Server) SetMessageHandler(handler func(ws Channel, data []byte) error) {
	server.messageHandler = handler
}

func (server *Server) SetCheckClientHandler(handler func(id string, r *http.Request) bool) {
	server.checkClientHandler = handler
}

func (server *Server) SetNewClientHandler(handler func(ws Channel)) {
	server.newClientHandler = handler
}

func (server *Server) SetDisconnectedClientHandler(handler func(ws Channel)) {
	server.disconnectedHandler = handler
}

func (server *Server) SetTimeoutConfig(config ServerTimeoutConfig) {
	server.timeoutConfig = config
}

func (server *Server) AddSupportedSubprotocol(subProto string) {
	for _, sub := range server.subprotocols {
		if sub == subProto {
			// Don't add duplicates
			return
		}
	}
	server.subprotocols = append(server.subprotocols, subProto)
}

func (server *Server) SetBasicAuthHandler(handler func(username string, password string) bool) {
	server.basicAuthHandler = handler
}

func (server *Server) SetCheckOriginHandler(handler func(r *http.Request) bool) {
	server.checkOrigin = handler
}

// error logs err and reports it on the Errors channel, if one was requested.
// Like ocpp-go's, the report waits for a reader of the channel, but it gives up
// once the server is stopped.
func (server *Server) error(err error) {
	log.Error(err)
	server.stateMu.Lock()
	c, stopped := server.errC, server.stopped
	server.stateMu.Unlock()
	if c == nil {
		return
	}
	// Stop closes `stopped`, then takes errMu for writing to close c: a sender
	// is either done by then or gives up, so c is never sent to once closed.
	server.errMu.RLock()
	defer server.errMu.RUnlock()
	select {
	case <-stopped:
		return
	default:
	}
	select {
	case c <- err:
	case <-stopped:
	}
}

func (server *Server) Errors() <-chan error {
	server.stateMu.Lock()
	defer server.stateMu.Unlock()
	if server.errC == nil {
		server.errC = make(chan error, 1)
	}
	return server.errC
}

func (server *Server) Addr() *net.TCPAddr {
	server.stateMu.Lock()
	defer server.stateMu.Unlock()
	return server.addr
}

func (server *Server) AddHttpHandler(listenPath string, handler func(w http.ResponseWriter, r *http.Request)) {
	server.httpHandler.HandleFunc(listenPath, handler)
}

func (server *Server) Start(port int, listenPath string) {
	server.connMutex.Lock()
	server.connections = make(map[string]*WebSocket)
	server.connMutex.Unlock()
	server.stateMu.Lock()
	if server.httpServer == nil || server.stopping.Load() {
		// First start after a Stop: an http.Server cannot serve again after Shutdown.
		server.httpServer = &http.Server{TLSConfig: server.tlsConfig}
		select {
		case <-server.stopped:
			server.stopped = make(chan struct{})
		default:
		}
		server.stopping.Store(false)
	}
	httpServer := server.httpServer
	server.stateMu.Unlock()

	addr := fmt.Sprintf(":%v", port)
	httpServer.Addr = addr

	server.AddHttpHandler(listenPath, func(w http.ResponseWriter, r *http.Request) {
		server.wsHandler(w, r)
	})
	httpServer.Handler = server.httpHandler

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		server.error(fmt.Errorf("failed to listen: %w", err))
		return
	}

	server.stateMu.Lock()
	server.addr = ln.Addr().(*net.TCPAddr)
	server.stateMu.Unlock()

	defer ln.Close()

	log.Infof("listening on tcp network %v", addr)
	httpServer.RegisterOnShutdown(server.stopConnections)
	if server.tlsCertificatePath != "" && server.tlsCertificateKey != "" {
		err = httpServer.ServeTLS(ln, server.tlsCertificatePath, server.tlsCertificateKey)
	} else {
		err = httpServer.Serve(ln)
	}

	if err != http.ErrServerClosed {
		server.error(fmt.Errorf("failed to listen: %w", err))
	}
}

func (server *Server) Stop() {
	log.Info("stopping websocket server")
	server.stopping.Store(true)
	server.stateMu.Lock()
	httpServer := server.httpServer
	server.stateMu.Unlock()
	err := httpServer.Shutdown(context.TODO())
	if err != nil {
		server.error(fmt.Errorf("shutdown failed: %w", err))
	}

	server.stateMu.Lock()
	select {
	case <-server.stopped:
	default:
		close(server.stopped)
	}
	server.stateMu.Unlock()
	server.errMu.Lock()
	server.stateMu.Lock()
	if server.errC != nil {
		close(server.errC)
		server.errC = nil
	}
	server.stateMu.Unlock()
	server.errMu.Unlock()
}

// Connections retrieves a WebSocket connection by its unique identifier, or nil.
func (server *Server) Connections(websocketId string) *WebSocket {
	server.connMutex.RLock()
	defer server.connMutex.RUnlock()
	return server.connections[websocketId]
}

func (server *Server) lookup(id string) (*WebSocket, bool) {
	server.connMutex.RLock()
	defer server.connMutex.RUnlock()
	ws, ok := server.connections[id]
	return ws, ok
}

func (server *Server) StopConnection(id string, closeError gorilla.CloseError) error {
	ws, ok := server.lookup(id)
	if !ok {
		return fmt.Errorf("couldn't stop websocket connection. No connection with id %s is open", id)
	}
	log.Debugf("sending stop signal for websocket %s", ws.ID())
	return ws.serverConn.requestClose(websocket.CloseError{Code: websocket.StatusCode(closeError.Code), Reason: closeError.Text})
}

// requestClose asks the connection to close with a close frame of the given code and text.
func (sc *serverConn) requestClose(ce websocket.CloseError) error {
	if sc.dc != nil {
		if sc.dc.Context().Err() != nil {
			return errors.New("socket is already closed")
		}
		sc.dc.AbortWithStatus(transport.StatusCode(ce.Code), ce.Reason)
		return nil
	}
	select {
	case <-sc.done:
		return errors.New("socket is already closed")
	default:
	}
	select {
	case sc.closeC <- ce:
		return nil
	default:
		return errors.New("close already signaled for socket")
	}
}

func (server *Server) stopConnections() {
	server.connMutex.RLock()
	conns := make([]*WebSocket, 0, len(server.connections))
	for _, conn := range server.connections {
		conns = append(conns, conn)
	}
	server.connMutex.RUnlock()
	for _, ws := range conns {
		_ = ws.serverConn.requestClose(websocket.CloseError{Code: websocket.StatusNormalClosure})
	}
}

// Write queues data for the given connection without ever blocking the whole
// server. Without the gocpp runtime the data goes to a bounded per-connection
// queue and a full queue is an error, as in ocpp-go. When the connection is
// served by the gocpp runtime the frame is written on the calling goroutine
// (bounded by WriteWait), serialized with the runtime's own writes by the
// WebSocket library.
func (server *Server) Write(webSocketId string, data []byte) error {
	ws, ok := server.lookup(webSocketId)
	if !ok {
		return fmt.Errorf("couldn't write to websocket. No socket with id %v is open", webSocketId)
	}
	log.Debugf("queuing data for websocket %s", webSocketId)
	sc := ws.serverConn
	if sc.dc != nil {
		if sc.dc.Context().Err() != nil {
			return fmt.Errorf("couldn't write to websocket. Socket with id %v is closed", webSocketId)
		}
		ctx, cancel := context.WithTimeout(context.Background(), server.writeWait())
		defer cancel()
		if err := sc.t.Write(ctx, data); err != nil {
			return fmt.Errorf("couldn't write to websocket %v: %w", webSocketId, err)
		}
		return nil
	}
	select {
	case <-sc.done:
		return fmt.Errorf("couldn't write to websocket. Socket with id %v is closed", webSocketId)
	default:
	}
	select {
	case sc.outQueue <- data:
		return nil
	default:
		return fmt.Errorf("couldn't write to websocket. Output queue for socket with id %v is full", webSocketId)
	}
}

func (server *Server) writeWait() time.Duration {
	if w := server.timeoutConfig.WriteWait; w > 0 {
		return w
	}
	return defaultWriteWait
}

// sameOrigin is gorilla's default origin check: a request with an Origin header
// is accepted only if its host equals the Host header.
func sameOrigin(r *http.Request) bool {
	origin := r.Header["Origin"]
	if len(origin) == 0 {
		return true
	}
	u, err := url.Parse(origin[0])
	if err != nil {
		return false
	}
	return strings.EqualFold(u.Host, r.Host)
}

func (server *Server) wsHandler(w http.ResponseWriter, r *http.Request) {
	id := path.Base(r.URL.Path)
	res, ok := server.admit(w, id)
	if !ok {
		return
	}
	defer server.releaseSlot(res)
	log.Debugf("handling new connection for %s from %s", id, r.RemoteAddr)
	// Negotiate sub-protocol: the first one the client offers that is supported
	// (any, if none were configured).
	clientSubprotocols := gorilla.Subprotocols(r)
	negotiated := ""
out:
	for _, requested := range clientSubprotocols {
		if len(server.subprotocols) == 0 {
			negotiated = requested
			break
		}
		for _, supported := range server.subprotocols {
			if requested == supported {
				negotiated = requested
				break out
			}
		}
	}
	// Handle client authentication
	if server.basicAuthHandler != nil {
		username, password, ok := r.BasicAuth()
		if ok {
			ok = server.basicAuthHandler(username, password)
		}
		if !ok {
			server.upgradeResult("auth_rejected")
			server.error(fmt.Errorf("basic auth failed: credentials invalid"))
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	if server.checkClientHandler != nil {
		ok := server.checkClientHandler(id, r)
		if !ok {
			server.upgradeResult("auth_rejected")
			server.error(fmt.Errorf("client validation: invalid client"))
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
	}

	check := sameOrigin
	if server.checkOrigin != nil {
		check = server.checkOrigin
	}
	if !check(r) {
		const msg = "websocket: request origin not allowed by Upgrader.CheckOrigin"
		http.Error(w, msg, http.StatusForbidden)
		server.upgradeResult("auth_rejected")
		server.error(fmt.Errorf("upgrade failed: %s", msg))
		return
	}

	rtBox := server.rt.Load()
	sc := &serverConn{srv: server}
	opts := &websocket.AcceptOptions{
		// Offering only the negotiated protocol keeps the choice ours; with none
		// the upgrade still succeeds and the connection is closed below, as in ocpp-go.
		Subprotocols:       nil,
		InsecureSkipVerify: true, // origin was checked above
		CompressionMode:    websocket.CompressionDisabled,
	}
	if negotiated != "" {
		opts.Subprotocols = []string{negotiated}
	}
	opts.OnPingReceived = func(context.Context, []byte) bool {
		sc.noteActivity()
		return true
	}
	opts.OnPongReceived = func(context.Context, []byte) { sc.noteActivity() }
	conn, err := websocket.Accept(newSmallBufWriter(w, defaultIOReadBuffer, defaultIOWriteBuffer), r, opts)
	if err != nil {
		server.upgradeResult("bad_request")
		server.error(fmt.Errorf("upgrade failed: %w", err))
		return
	}
	limit := serverMaxMessageSize(server.timeoutConfig)
	if limit <= 0 {
		limit = math.MaxInt64 >> 1
	}
	sc.c = conn
	sc.t = transport.NewCoderWSWithLimit(conn, limit)
	ws := &WebSocket{
		id:                 id,
		tlsConnectionState: r.TLS,
		remote:             remoteAddr(r),
		serverConn:         sc,
	}
	log.Debugf("upgraded websocket connection for %s from %s", id, r.RemoteAddr)
	// If unsupported subprotocol, terminate the connection immediately
	if negotiated == "" {
		server.upgradeResult("subprotocol")
		server.error(fmt.Errorf("unsupported subprotocols %v for new client %v (%v)", clientSubprotocols, id, r.RemoteAddr))
		_ = conn.Close(websocket.StatusProtocolError, "invalid or unsupported subprotocol")
		return
	}
	if rtBox != nil {
		sc.dc = rtBox.rt.Open(compatrt.Conn{
			Channel:     ws,
			WS:          sc.t,
			Subprotocol: negotiated,
			RemoteAddr:  r.RemoteAddr,
			TLS:         r.TLS,
		})
	} else {
		sc.outQueue = make(chan []byte, outQueueSize)
		sc.closeC = make(chan websocket.CloseError, 1)
		sc.done = make(chan struct{})
	}
	server.upgradeResult("ok")
	// Check whether client exists. Last-wins policy: the NEW connection takes
	// over the slot and the old one is closed. A charger that lost its previous
	// connection re-dials, and a stale (often already dead) entry must never
	// lock it out.
	server.connMutex.Lock()
	if server.connections == nil {
		server.connections = make(map[string]*WebSocket)
	}
	oldWs, exists := server.connections[id]
	server.connections[id] = ws
	server.consumeSlotLocked(res)
	server.connMutex.Unlock()
	if exists {
		server.error(fmt.Errorf("client %s already exists, closing stale connection and keeping the new one", id))
		oldWs.serverConn.replaced()
		// Notify the application of the old connection's termination BEFORE the
		// new-client handler below, preserving the disconnect -> connect order the
		// application layer expects.
		if server.disconnectedHandler != nil {
			server.disconnectedHandler(oldWs)
		}
	}
	if server.newClientHandler != nil {
		var channel Channel = ws
		server.newClientHandler(channel)
	}
	if sc.dc != nil {
		sc.dc.ReleaseInbound()
		// From here the connection's lifetime is not tied to this HTTP handler.
		context.AfterFunc(sc.dc.Context(), func() { sc.finish(ws) })
		return
	}
	sc.idle = time.AfterFunc(idleTimeout(server.timeoutConfig.PingWait), func() { _ = sc.c.CloseNow() })
	if server.timeoutConfig.PingWait == 0 {
		sc.idle.Stop()
	}
	go sc.writeLoop(ws)
	go sc.readLoop(ws)
}

func idleTimeout(d time.Duration) time.Duration {
	if d <= 0 {
		return time.Hour
	}
	return d
}

func remoteAddr(r *http.Request) net.Addr {
	if a, err := net.ResolveTCPAddr("tcp", r.RemoteAddr); err == nil {
		return a
	}
	return strAddr(r.RemoteAddr)
}

type strAddr string

func (a strAddr) Network() string { return "tcp" }
func (a strAddr) String() string  { return string(a) }

// replaced closes a connection that a newer one with the same id took over.
func (sc *serverConn) replaced() {
	const reason = "replaced by a new connection with the same ID"
	if sc.dc != nil {
		sc.dc.AbortWithStatus(transport.StatusPolicyViolation, reason)
		return
	}
	// Politely close the old connection; its read loop then ends and runs finish,
	// whose pointer guard keeps it from deleting the new entry.
	go func() { _ = sc.c.Close(websocket.StatusPolicyViolation, reason) }()
}

func (sc *serverConn) noteActivity() {
	if sc.dc != nil {
		sc.dc.NoteActivity()
		return
	}
	if sc.idle != nil && sc.srv.timeoutConfig.PingWait > 0 {
		sc.idle.Reset(sc.srv.timeoutConfig.PingWait)
	}
}

// readLoop is the read side of a connection not served by the dispatcher.
func (sc *serverConn) readLoop(ws *WebSocket) {
	s := sc.srv
	ctx := context.Background()
	for {
		_, message, err := sc.c.Read(ctx)
		if err != nil {
			switch websocket.CloseStatus(err) {
			case websocket.StatusGoingAway, websocket.StatusAbnormalClosure, websocket.StatusNormalClosure, websocket.StatusNoStatusRcvd:
			default:
				if websocket.CloseStatus(err) != -1 {
					s.error(fmt.Errorf("read failed unexpectedly for %s: %w", ws.ID(), err))
				}
			}
			log.Debugf("handling read error for %s: %v", ws.ID(), err.Error())
			sc.finish(ws)
			return
		}
		if s.messageHandler != nil {
			var channel Channel = ws
			if err = s.messageHandler(channel, message); err != nil {
				s.error(fmt.Errorf("handling failed for %s: %w", ws.ID(), err))
				continue
			}
		}
		sc.noteActivity()
	}
}

// writeLoop is the write side of a connection not served by the dispatcher.
func (sc *serverConn) writeLoop(ws *WebSocket) {
	s := sc.srv
	for {
		select {
		case data := <-sc.outQueue:
			ctx, cancel := context.WithTimeout(context.Background(), s.writeWait())
			err := sc.c.Write(ctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				s.error(fmt.Errorf("write failed for %s: %w", ws.ID(), err))
				sc.finish(ws)
				return
			}
			log.Debugf("written %d bytes to %s", len(data), ws.ID())
		case ce := <-sc.closeC:
			log.Debugf("closing connection to %s", ws.ID())
			if err := sc.c.Close(ce.Code, ce.Reason); err != nil && websocket.CloseStatus(err) == -1 {
				s.error(fmt.Errorf("failed to write close message for connection %s: %w", ws.ID(), err))
			}
			sc.finish(ws)
			return
		case <-sc.done:
			return
		}
	}
}

// finish frees the resources of a connection that has ended and tells the
// application, once. From this moment on no new messages may be sent.
func (sc *serverConn) finish(ws *WebSocket) {
	sc.cleanup.Do(func() {
		s := sc.srv
		if sc.dc != nil {
			// ocpp-go logs why a connection's read loop ended; keep the line (the
			// text is the dispatcher's read error) unless the server closed it.
			var cs *dispatcher.CloseStatus
			if cause := context.Cause(sc.dc.Context()); cause != nil && !errors.As(cause, &cs) &&
				!errors.Is(cause, gocppj.ErrConnClosedByServer) {
				log.Debugf("handling read error for %s: %v", ws.ID(), cause)
			}
			_ = sc.dc.Close(nil)
		} else {
			_ = sc.c.CloseNow()
			close(sc.done)
			if sc.idle != nil {
				sc.idle.Stop()
			}
		}
		s.connMutex.Lock()
		// Pointer guard: only delete the map entry if it still belongs to THIS
		// connection. Under the last-wins policy a newer connection may have taken
		// over the id, and the takeover path already told the application.
		replaced := s.connections[ws.id] != ws
		if !replaced {
			delete(s.connections, ws.id)
		}
		s.connMutex.Unlock()
		if replaced {
			log.Infof("closed stale connection to %s (id taken over by a newer connection)", ws.ID())
			return
		}
		log.Infof("closed connection to %s", ws.ID())
		if s.disconnectedHandler != nil {
			s.disconnectedHandler(ws)
		}
	})
}

// serverMaxMessageSize is the inbound message size limit of a ServerTimeoutConfig.
// ocpp-go v0.19 has no such setting (as gorilla/websocket, it reads without a limit).
func serverMaxMessageSize(ServerTimeoutConfig) int64 { return 0 }
