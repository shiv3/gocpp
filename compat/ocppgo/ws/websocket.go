// The package is a wrapper around gorilla websockets,
// aimed at simplifying the creation and usage of a websocket client/server.
//
// Check the Client and Server structure to get started.
package ws

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/logging"
)

const (
	// Time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second
	// Default upper bound for a single incoming message on the server side.
	// OCPP frames are small JSON documents; without a limit gorilla/websocket
	// buffers a frame of any size, so one client can exhaust process memory.
	defaultServerMaxMessageSize = 1 << 20
	// Time allowed to read the next pong message from the peer.
	defaultPongWait = 60 * time.Second
	// Time allowed to wait for a ping on the server, before closing a connection due to inactivity.
	defaultPingWait = defaultPongWait
	// Send pings to peer with this period. Must be less than pongWait.
	defaultPingPeriod = (defaultPongWait * 9) / 10
	// Time allowed for the initial handshake to complete.
	defaultHandshakeTimeout = 30 * time.Second
	// The base delay to be used for automatic reconnection. Will double for every attempt up to maxReconnectionDelay.
	defaultReconnectBackoff = 5 * time.Second
	// Default maximum reconnection delay for websockets
	defaultReconnectMaxBackoff = 2 * time.Minute
)

// The internal verbose logger
var log logging.Logger

// Sets a custom Logger implementation, allowing the package to log events.
// By default, a VoidLogger is used, so no logs will be sent to any output.
//
// The function panics, if a nil logger is passed.
func SetLogger(logger logging.Logger) {
	if logger == nil {
		panic("cannot set a nil logger")
	}
	log = logger
}

// Config contains optional configuration parameters for a websocket server.
// Setting the parameter allows to define custom timeout intervals for websocket network operations.
//
// To set a custom configuration, refer to the server's SetTimeoutConfig method.
// If no configuration is passed, a default configuration is generated via the NewServerTimeoutConfig function.
type ServerTimeoutConfig struct {
	WriteWait time.Duration
	PingWait  time.Duration
	// MaxMessageSize is the maximum size in bytes of a message read from a client.
	// A larger message closes the connection. Zero or a negative value disables the limit.
	MaxMessageSize int64
}

// NewServerTimeoutConfig creates a default timeout configuration for a websocket endpoint.
//
// You may change fields arbitrarily and pass the struct to a SetTimeoutConfig method.
func NewServerTimeoutConfig() ServerTimeoutConfig {
	return ServerTimeoutConfig{WriteWait: defaultWriteWait, PingWait: defaultPingWait, MaxMessageSize: defaultServerMaxMessageSize}
}

// Config contains optional configuration parameters for a websocket client.
// Setting the parameter allows to define custom timeout intervals for websocket network operations.
//
// To set a custom configuration, refer to the client's SetTimeoutConfig method.
// If no configuration is passed, a default configuration is generated via the NewClientTimeoutConfig function.
type ClientTimeoutConfig struct {
	WriteWait           time.Duration
	HandshakeTimeout    time.Duration
	PongWait            time.Duration
	PingPeriod          time.Duration
	ReconnectBackoff    time.Duration
	ReconnectMaxBackoff time.Duration
}

// NewClientTimeoutConfig creates a default timeout configuration for a websocket endpoint.
//
// You may change fields arbitrarily and pass the struct to a SetTimeoutConfig method.
func NewClientTimeoutConfig() ClientTimeoutConfig {
	return ClientTimeoutConfig{
		WriteWait:           defaultWriteWait,
		HandshakeTimeout:    defaultHandshakeTimeout,
		PongWait:            defaultPongWait,
		PingPeriod:          defaultPingPeriod,
		ReconnectBackoff:    defaultReconnectBackoff,
		ReconnectMaxBackoff: defaultReconnectMaxBackoff,
	}
}

// Channel represents a bi-directional communication channel, which provides at least a unique ID.
type Channel interface {
	ID() string
	RemoteAddr() net.Addr
	TLSConnectionState() *tls.ConnectionState
}

// WebSocket is a wrapper for a single websocket channel.
// The connection itself is provided by the gorilla websocket package.
//
// Don't use a websocket directly, but refer to WsServer and WsClient.
type WebSocket struct {
	connection         *websocket.Conn
	id                 string
	outQueue           chan []byte
	closeC             chan websocket.CloseError // used to gracefully close a websocket connection.
	forceCloseC        chan error                // used by the readPump to notify a forcefully closed connection to the writePump.
	pingMessage        chan []byte
	tlsConnectionState *tls.ConnectionState
	// sendMutex guards sends on outQueue/closeC against a concurrent channel
	// close in cleanupConnection, so a late Write/StopConnection can never
	// panic on a closed channel.
	sendMutex sync.Mutex
	// closed is set (under sendMutex) once the connection's channels have
	// been closed.
	closed bool
	// remote is the peer address of a connection accepted by a Server, whose
	// connection is not a gorilla one (see server.go).
	remote net.Addr
	// serverConn is non-nil for a connection accepted by a Server.
	serverConn *serverConn
}

// Retrieves the unique Identifier of the websocket (typically, the URL suffix).
func (websocket *WebSocket) ID() string {
	return websocket.id
}

// Returns the address of the remote peer.
func (websocket *WebSocket) RemoteAddr() net.Addr {
	if websocket.connection == nil {
		return websocket.remote
	}
	return websocket.connection.RemoteAddr()
}

// Returns the TLS connection state of the connection, if any.
func (websocket *WebSocket) TLSConnectionState() *tls.ConnectionState {
	return websocket.tlsConnectionState
}

// ConnectionError is a websocket
type HttpConnectionError struct {
	Message    string
	HttpStatus string
	HttpCode   int
	Details    string
}

func (e HttpConnectionError) Error() string {
	return fmt.Sprintf("%v, http status: %v", e.Message, e.HttpStatus)
}

// ---------------------- SERVER ----------------------

type CheckClientHandler func(id string, r *http.Request) bool

// WsServer defines a websocket server, which passively listens for incoming connections on ws or wss protocol.
// The offered API are of asynchronous nature, and each incoming connection/message is handled using callbacks.
//
// To create a new ws server, use:
//
//	server := NewServer()
//
// If you need a TLS ws server instead, use:
//
//	server := NewTLSServer("cert.pem", "privateKey.pem")
//
// To support client basic authentication, use:
//
//	server.SetBasicAuthHandler(func (user, pass) bool {
//		ok := authenticate(user, pass) // ... check for user and pass correctness
//		return ok
//	})
//
// To specify supported sub-protocols, use:
//
//	server.AddSupportedSubprotocol("ocpp1.6")
//
// If you need to set a specific timeout configuration, refer to the SetTimeoutConfig method.
//
// Using Start and Stop you can respectively start and stop listening for incoming client websocket connections.
//
// To be notified of new and terminated connections,
// refer to SetNewClientHandler and SetDisconnectedClientHandler functions.
//
// To receive incoming messages, you will need to set your own handler using SetMessageHandler.
// To write data on the open socket, simply call the Write function.
type WsServer interface {
	// Starts and runs the websocket server on a specific port and URL.
	// After start, incoming connections and messages are handled automatically, so no explicit read operation is required.
	//
	// The functions blocks forever, hence it is suggested to invoke it in a goroutine, if the caller thread needs to perform other work, e.g.:
	//	go server.Start(8887, "/ws/{id}")
	//	doStuffOnMainThread()
	//	...
	//
	// To stop a running server, call the Stop function.
	Start(port int, listenPath string)
	// Shuts down a running websocket server.
	// All open channels will be forcefully closed, and the previously called Start function will return.
	Stop()
	// Closes a specific websocket connection.
	StopConnection(id string, closeError websocket.CloseError) error
	// Errors returns a channel for error messages. If it doesn't exist it es created.
	// The channel is closed by the server when stopped.
	Errors() <-chan error
	// Sets a callback function for all incoming messages.
	// The callbacks accept a Channel and the received data.
	// It is up to the callback receiver, to check the identifier of the channel, to determine the source of the message.
	SetMessageHandler(handler func(ws Channel, data []byte) error)
	// Sets a callback function for all new incoming client connections.
	// It is recommended to store a reference to the Channel in the received entity, so that the Channel may be recognized later on.
	SetNewClientHandler(handler func(ws Channel))
	// Sets a callback function for all client disconnection events.
	// Once a client is disconnected, it is not possible to read/write on the respective Channel any longer.
	SetDisconnectedClientHandler(handler func(ws Channel))
	// Set custom timeout configuration parameters. If not passed, a default ServerTimeoutConfig struct will be used.
	//
	// This function must be called before starting the server, otherwise it may lead to unexpected behavior.
	SetTimeoutConfig(config ServerTimeoutConfig)
	// Sends a message on a specific Channel, identifier by the webSocketId parameter.
	// If the passed ID is invalid, an error is returned.
	//
	// The data is queued and will be sent asynchronously in the background.
	Write(webSocketId string, data []byte) error
	// Adds support for a specified subprotocol.
	// This is recommended in order to communicate the capabilities to the client during the handshake.
	// If left empty, any subprotocol will be accepted.
	//
	// Duplicates will be removed automatically.
	AddSupportedSubprotocol(subProto string)
	// SetBasicAuthHandler enables HTTP Basic Authentication and requires clients to pass credentials.
	// The handler function is called whenever a new client attempts to connect, to check for credentials correctness.
	// The handler must return true if the credentials were correct, false otherwise.
	SetBasicAuthHandler(handler func(username string, password string) bool)
	// SetCheckOriginHandler sets a handler for incoming websocket connections, allowing to perform
	// custom cross-origin checks.
	//
	// By default, if the Origin header is present in the request, and the Origin host is not equal
	// to the Host request header, the websocket handshake fails.
	SetCheckOriginHandler(handler func(r *http.Request) bool)
	// SetCheckClientHandler sets a handler for validate incoming websocket connections, allowing to perform
	// custom client connection checks.
	SetCheckClientHandler(handler func(id string, r *http.Request) bool)
	// Addr gives the address on which the server is listening, useful if, for
	// example, the port is system-defined (set to 0).
	Addr() *net.TCPAddr
}

// ---------------------- CLIENT ----------------------

// WsClient defines a websocket client, needed to connect to a websocket server.
// The offered API are of asynchronous nature, and each incoming message is handled using callbacks.
//
// To create a new ws client, use:
//
//	client := NewClient()
//
// If you need a TLS ws client instead, use:
//
//	certPool, err := x509.SystemCertPool()
//	if err != nil {
//		log.Fatal(err)
//	}
//	// You may add more trusted certificates to the pool before creating the TLSClientConfig
//	client := NewTLSClient(&tls.Config{
//		RootCAs: certPool,
//	})
//
// To add additional dial options, use:
//
//	client.AddOption(func(*websocket.Dialer) {
//		// Your option ...
//	)}
//
// To add basic HTTP authentication, use:
//
//	client.SetBasicAuth("username","password")
//
// If you need to set a specific timeout configuration, refer to the SetTimeoutConfig method.
//
// Using Start and Stop you can respectively open/close a websocket to a websocket server.
//
// To receive incoming messages, you will need to set your own handler using SetMessageHandler.
// To write data on the open socket, simply call the Write function.
type WsClient interface {
	// Starts the client and attempts to connect to the server on a specified URL.
	// If the connection fails, an error is returned.
	//
	// For example:
	//	err := client.Start("ws://localhost:8887/ws/1234")
	//
	// The function returns immediately, after the connection has been established.
	// Incoming messages are passed automatically to the callback function, so no explicit read operation is required.
	//
	// To stop a running client, call the Stop function.
	Start(url string) error
	// Closes the output of the websocket Channel, effectively closing the connection to the server with a normal closure.
	Stop()
	// Errors returns a channel for error messages. If it doesn't exist it es created.
	// The channel is closed by the client when stopped.
	Errors() <-chan error
	// Sets a callback function for all incoming messages.
	SetMessageHandler(handler func(data []byte) error)
	// Set custom timeout configuration parameters. If not passed, a default ClientTimeoutConfig struct will be used.
	//
	// This function must be called before connecting to the server, otherwise it may lead to unexpected behavior.
	SetTimeoutConfig(config ClientTimeoutConfig)
	// Sets a callback function for receiving notifications about an unexpected disconnection from the server.
	// The callback is invoked even if the automatic reconnection mechanism is active.
	//
	// If the client was stopped using the Stop function, the callback will NOT be invoked.
	SetDisconnectedHandler(handler func(err error))
	// Sets a callback function for receiving notifications whenever the connection to the server is re-established.
	// Connections are re-established automatically thanks to the auto-reconnection mechanism.
	//
	// If set, the DisconnectedHandler will always be invoked before the Reconnected callback is invoked.
	SetReconnectedHandler(handler func())
	// IsConnected Returns information about the current connection status.
	// If the client is currently attempting to auto-reconnect to the server, the function returns false.
	IsConnected() bool
	// Sends a message to the server over the websocket.
	//
	// The data is queued and will be sent asynchronously in the background.
	Write(data []byte) error
	// Adds a websocket option to the client.
	AddOption(option interface{})
	// SetRequestedSubProtocol will negotiate the specified sub-protocol during the websocket handshake.
	// Internally this creates a dialer option and invokes the AddOption method on the client.
	//
	// Duplicates generated by invoking this method multiple times will be ignored.
	SetRequestedSubProtocol(subProto string)
	// SetBasicAuth adds basic authentication credentials, to use when connecting to the server.
	// The credentials are automatically encoded in base64.
	SetBasicAuth(username string, password string)
	// SetHeaderValue sets a value on the HTTP header sent when opening a websocket connection to the server.
	//
	// The function overwrites previous header fields with the same key.
	SetHeaderValue(key string, value string)
}

// Client is the default implementation of a Websocket client.
//
// Use the NewClient or NewTLSClient functions to create a new client.
type Client struct {
	webSocket      WebSocket
	url            url.URL
	messageHandler func(data []byte) error
	dialOptions    []func(*websocket.Dialer)
	header         http.Header
	timeoutConfig  ClientTimeoutConfig
	connected      bool
	onDisconnected func(err error)
	onReconnected  func()
	mutex          sync.Mutex
	errC           chan error
	reconnectC     chan struct{} // used for signaling, that a reconnection attempt should be interrupted
}

// Creates a new simple websocket client (the channel is not secured).
//
// Additional options may be added using the AddOption function.
//
// Basic authentication can be set using the SetBasicAuth function.
//
// By default, the client will not neogtiate any subprotocol. This value needs to be set via the
// respective SetRequestedSubProtocol method.
func NewClient() *Client {
	return &Client{
		dialOptions:   []func(*websocket.Dialer){},
		timeoutConfig: NewClientTimeoutConfig(),
		header:        http.Header{},
	}
}

// NewTLSClient creates a new secure websocket client. If supported by the server, the websocket channel will use TLS.
//
// Additional options may be added using the AddOption function.
// Basic authentication can be set using the SetBasicAuth function.
//
// To set a client certificate, you may do:
//
//	certificate, _ := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
//	clientCertificates := []tls.Certificate{certificate}
//	client := ws.NewTLSClient(&tls.Config{
//		RootCAs:      certPool,
//		Certificates: clientCertificates,
//	})
//
// You can set any other TLS option within the same constructor as well.
// For example, if you wish to test connecting to a server having a
// self-signed certificate (do not use in production!), pass:
//
//	InsecureSkipVerify: true
func NewTLSClient(tlsConfig *tls.Config) *Client {
	client := &Client{dialOptions: []func(*websocket.Dialer){}, timeoutConfig: NewClientTimeoutConfig(), header: http.Header{}}
	client.dialOptions = append(client.dialOptions, func(dialer *websocket.Dialer) {
		dialer.TLSClientConfig = tlsConfig
	})
	return client
}

func (client *Client) SetMessageHandler(handler func(data []byte) error) {
	client.messageHandler = handler
}

func (client *Client) SetTimeoutConfig(config ClientTimeoutConfig) {
	client.timeoutConfig = config
}

func (client *Client) SetDisconnectedHandler(handler func(err error)) {
	client.onDisconnected = handler
}

func (client *Client) SetReconnectedHandler(handler func()) {
	client.onReconnected = handler
}

func (client *Client) AddOption(option interface{}) {
	dialOption, ok := option.(func(*websocket.Dialer))
	if ok {
		client.dialOptions = append(client.dialOptions, dialOption)
	}
}

func (client *Client) SetRequestedSubProtocol(subProto string) {
	opt := func(dialer *websocket.Dialer) {
		alreadyExists := false
		for _, proto := range dialer.Subprotocols {
			if proto == subProto {
				alreadyExists = true
				break
			}
		}
		if !alreadyExists {
			dialer.Subprotocols = append(dialer.Subprotocols, subProto)
		}
	}
	client.AddOption(opt)
}

func (client *Client) SetBasicAuth(username string, password string) {
	client.header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(username+":"+password)))
}

func (client *Client) SetHeaderValue(key string, value string) {
	client.header.Set(key, value)
}

func (client *Client) getReadTimeout() time.Time {
	if client.timeoutConfig.PongWait == 0 {
		return time.Time{}
	}
	return time.Now().Add(client.timeoutConfig.PongWait)
}

func (client *Client) writePump() {
	ticker := time.NewTicker(client.timeoutConfig.PingPeriod)
	conn := client.webSocket.connection
	// Closure function correctly closes the current connection
	closure := func(err error) {
		ticker.Stop()
		client.cleanup()
		// Invoke callback
		if client.onDisconnected != nil {
			client.onDisconnected(err)
		}
	}

	for {
		select {
		case data := <-client.webSocket.outQueue:
			// Send data
			log.Debugf("sending data")
			_ = conn.SetWriteDeadline(time.Now().Add(client.timeoutConfig.WriteWait))
			err := conn.WriteMessage(websocket.TextMessage, data)
			if err != nil {
				client.error(fmt.Errorf("write failed: %w", err))
				closure(err)
				client.handleReconnection()
				return
			}
			log.Debugf("written %d bytes", len(data))
		case <-ticker.C:
			// Send periodic ping
			_ = conn.SetWriteDeadline(time.Now().Add(client.timeoutConfig.WriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				client.error(fmt.Errorf("failed to send ping message: %w", err))
				closure(err)
				client.handleReconnection()
				return
			}
			log.Debugf("ping sent")
		case closeErr := <-client.webSocket.closeC:
			log.Debugf("closing connection")
			// Closing connection gracefully
			if err := conn.WriteControl(
				websocket.CloseMessage,
				websocket.FormatCloseMessage(closeErr.Code, closeErr.Text),
				time.Now().Add(client.timeoutConfig.WriteWait),
			); err != nil {
				client.error(fmt.Errorf("failed to write close message: %w", err))
			}
			// Disconnected by user command. Not calling auto-reconnect.
			// Passing nil will also not call onDisconnected.
			closure(nil)
			return
		case closed, ok := <-client.webSocket.forceCloseC:
			log.Debugf("handling forced close signal")
			// Read pump sent a forceClose signal (reading failed -> aborting the connection)
			if !ok || closed != nil {
				closure(closed)
				client.handleReconnection()
				return
			}
		}
	}
}

func (client *Client) readPump() {
	conn := client.webSocket.connection
	_ = conn.SetReadDeadline(client.getReadTimeout())
	conn.SetPongHandler(func(string) error {
		log.Debugf("pong received")
		return conn.SetReadDeadline(client.getReadTimeout())
	})
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure, websocket.CloseNormalClosure) {
				client.error(fmt.Errorf("read failed: %w", err))
			}
			// Notify writePump of error. Forced close will be handled there
			client.webSocket.forceCloseC <- err
			return
		}

		log.Debugf("received %v bytes", len(message))
		if client.messageHandler != nil {
			err = client.messageHandler(message)
			if err != nil {
				client.error(fmt.Errorf("handle failed: %w", err))
				continue
			}
		}
	}
}

// Frees internal resources after a websocket connection was signaled to be closed.
// From this moment onwards, no new messages may be sent.
func (client *Client) cleanup() {
	client.setConnected(false)
	ws := &client.webSocket
	_ = ws.connection.Close()
	client.mutex.Lock()
	defer client.mutex.Unlock()
	ws.sendMutex.Lock()
	defer ws.sendMutex.Unlock()
	if ws.closed {
		return
	}
	ws.closed = true
	close(ws.outQueue)
	close(ws.closeC)
}

func (client *Client) handleReconnection() {
	log.Info("started automatic reconnection handler")
	delay := client.timeoutConfig.ReconnectBackoff
	for {
		// Wait before reconnecting
		select {
		case <-time.After(delay):
		case <-client.reconnectC:
			return
		}

		err := client.Start(client.url.String())
		if err == nil {
			// Re-connection was successful
			log.Info("reconnected successfully to server")
			if client.onReconnected != nil {
				client.onReconnected()
			}
			return
		}
		client.error(fmt.Errorf("reconnection failed: %w", err))
		// Re-connection failed, double the delay
		delay *= 2
		if delay >= client.timeoutConfig.ReconnectMaxBackoff {
			delay = client.timeoutConfig.ReconnectMaxBackoff
		}
	}
}

func (client *Client) setConnected(connected bool) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	client.connected = connected
}

func (client *Client) IsConnected() bool {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.connected
}

// Write queues data for the server.
//
// The connection can go away between the connected check and the send — a
// server-side disconnect while a heartbeat is on its way out is an ordinary
// event on a mobile link — so the send is guarded the same way the server side
// is: a caller that loses the race gets an error instead of taking the process
// down on a closed channel.
func (client *Client) Write(data []byte) error {
	if !client.IsConnected() {
		return fmt.Errorf("client is currently not connected, cannot send data")
	}
	// Pointer: WebSocket is a value field, and the guard has to act on the
	// client's own socket rather than on a copy of it.
	ws := &client.webSocket
	ws.sendMutex.Lock()
	defer ws.sendMutex.Unlock()
	if ws.closed {
		return fmt.Errorf("client is currently not connected, cannot send data")
	}
	log.Debugf("queuing data for server")
	// Non-blocking, like the server side: blocking here would hold the guard while
	// the writePump is gone, and cleanup would then wait on it forever. A full
	// queue means this connection is stalled, which the caller handles as a write
	// failure.
	select {
	case ws.outQueue <- data:
		return nil
	default:
		return fmt.Errorf("couldn't write to websocket. Send queue is full")
	}
}

func (client *Client) Start(urlStr string) error {
	url, err := url.Parse(urlStr)
	if err != nil {
		return err
	}

	dialer := websocket.Dialer{
		ReadBufferSize:   1024,
		WriteBufferSize:  1024,
		HandshakeTimeout: client.timeoutConfig.HandshakeTimeout,
		Subprotocols:     []string{},
	}
	for _, option := range client.dialOptions {
		option(&dialer)
	}
	// Connect
	log.Info("connecting to server")
	ws, resp, err := dialer.Dial(urlStr, client.header)
	if err != nil {
		if resp != nil {
			httpError := HttpConnectionError{Message: err.Error(), HttpStatus: resp.Status, HttpCode: resp.StatusCode}
			// Parse http response details
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			if body != nil {
				httpError.Details = string(body)
			}
			err = httpError
		}
		return err
	}

	// The id of the charge point is the final path element
	id := path.Base(url.Path)
	client.url = *url
	client.webSocket = WebSocket{
		connection: ws,
		id:         id,
		// Room to absorb a burst while the writePump drains; the send itself is
		// non-blocking, so this is the buffer the caller writes into.
		outQueue:           make(chan []byte, 32),
		closeC:             make(chan websocket.CloseError, 1),
		forceCloseC:        make(chan error, 1),
		tlsConnectionState: resp.TLS,
	}
	log.Infof("connected to server as %s", id)
	client.reconnectC = make(chan struct{})
	client.setConnected(true)
	//Start reader and write routine
	go client.writePump()
	go client.readPump()
	return nil
}

func (client *Client) Stop() {
	log.Infof("closing connection to server")
	client.mutex.Lock()
	if client.connected {
		client.connected = false
		// Send signal for gracefully shutting down the connection
		select {
		case client.webSocket.closeC <- websocket.CloseError{Code: websocket.CloseNormalClosure, Text: ""}:
		default:
		}
	}
	client.mutex.Unlock()
	// Notify reconnection goroutine to stop (if any)
	if client.reconnectC != nil {
		close(client.reconnectC)
	}
	if client.errC != nil {
		close(client.errC)
		client.errC = nil
	}
	// Wait for connection to actually close
}

func (client *Client) error(err error) {
	log.Error(err)
	if client.errC != nil {
		client.errC <- err
	}
}

func (client *Client) Errors() <-chan error {
	if client.errC == nil {
		client.errC = make(chan error, 1)
	}
	return client.errC
}

func init() {
	log = &logging.VoidLogger{}
}
