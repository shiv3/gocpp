// The package is a wrapper around gorilla websockets,
// aimed at simplifying the creation and usage of a websocket client/server.
//
// Check the Client and Server structure to get started.
package ws

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"path"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lorenzodonini/ocpp-go/internal/synclog"
	"github.com/lorenzodonini/ocpp-go/logging"
)

const (
	// Time allowed to write a message to the peer.
	defaultWriteWait = 10 * time.Second
	// Time allowed to read the next pong message from the peer.
	defaultPongWait = 60 * time.Second
	// Time allowed to wait for a ping on the server, before closing a connection due to inactivity.
	defaultPingWait = defaultPongWait
	// Send pings to peer with this period. Must be less than pongWait.
	defaultPingPeriod = (defaultPongWait * 9) / 10
	// Time allowed for the initial handshake to complete.
	defaultHandshakeTimeout = 30 * time.Second
	// When the Charging Station is reconnecting, after a connection loss, it will use this variable for the amount of time
	// it will double the previous back-off time. When the maximum number of increments is reached, the Charging
	// Station keeps connecting with the same back-off time.
	defaultRetryBackOffRepeatTimes = 5
	// When the Charging Station is reconnecting, after a connection loss, it will use this variable as the maximum value
	// for the random part of the back-off time. It will add a new random value to every increasing back-off time,
	// including the first connection attempt (with this maximum), for the amount of times it will double the previous
	// back-off time. When the maximum number of increments is reached, the Charging Station will keep connecting
	// with the same back-off time.
	defaultRetryBackOffRandomRange = 15 // seconds
	// When the Charging Station is reconnecting, after a connection loss, it will use this variable as the minimum backoff
	// time, the first time it tries to reconnect.
	defaultRetryBackOffWaitMinimum = 10 * time.Second
)

// The internal verbose logger. SetLogger may be called at any time.
var (
	logTarget                = synclog.New()
	log       logging.Logger = logTarget
)

// Sets a custom Logger implementation, allowing the package to log events.
// By default, a VoidLogger is used, so no logs will be sent to any output.
//
// The function panics, if a nil logger is passed.
func SetLogger(logger logging.Logger) {
	if logger == nil {
		panic("cannot set a nil logger")
	}
	logTarget.Set(logger)
}

// Config contains optional configuration parameters for a websocket server.
// Setting the parameter allows to define custom timeout intervals for websocket network operations.
//
// To set a custom configuration, refer to the server's SetTimeoutConfig method.
// If no configuration is passed, a default configuration is generated via the NewServerTimeoutConfig function.
type ServerTimeoutConfig struct {
	WriteWait time.Duration
	PingWait  time.Duration
}

// NewServerTimeoutConfig creates a default timeout configuration for a websocket endpoint.
//
// You may change fields arbitrarily and pass the struct to a SetTimeoutConfig method.
func NewServerTimeoutConfig() ServerTimeoutConfig {
	return ServerTimeoutConfig{WriteWait: defaultWriteWait, PingWait: defaultPingWait}
}

// Config contains optional configuration parameters for a websocket client.
// Setting the parameter allows to define custom timeout intervals for websocket network operations.
//
// To set a custom configuration, refer to the client's SetTimeoutConfig method.
// If no configuration is passed, a default configuration is generated via the NewClientTimeoutConfig function.
type ClientTimeoutConfig struct {
	WriteWait               time.Duration
	HandshakeTimeout        time.Duration
	PongWait                time.Duration
	PingPeriod              time.Duration
	RetryBackOffRepeatTimes int
	RetryBackOffRandomRange int
	RetryBackOffWaitMinimum time.Duration
}

// NewClientTimeoutConfig creates a default timeout configuration for a websocket endpoint.
//
// You may change fields arbitrarily and pass the struct to a SetTimeoutConfig method.
func NewClientTimeoutConfig() ClientTimeoutConfig {
	return ClientTimeoutConfig{
		WriteWait:               defaultWriteWait,
		HandshakeTimeout:        defaultHandshakeTimeout,
		PongWait:                defaultPongWait,
		PingPeriod:              defaultPingPeriod,
		RetryBackOffRepeatTimes: defaultRetryBackOffRepeatTimes,
		RetryBackOffRandomRange: defaultRetryBackOffRandomRange,
		RetryBackOffWaitMinimum: defaultRetryBackOffWaitMinimum,
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
	doneC              chan struct{}             // client side: closed when the connection was cleaned up; writers must not use outQueue afterwards.
	pingMessage        chan []byte
	tlsConnectionState *tls.ConnectionState
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
	if websocket == nil {
		return nil
	}
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
	// Connections retrieves a WebSocket connection by its unique identifier.
	// If a connection with the given ID exists, it returns the corresponding WebSocket instance.
	// If no connection is found with the specified ID, it returns nil.
	Connections(websocketId string) *WebSocket
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
	// Starts the client and attempts to connect to the server on a specified URL.
	// If the connection fails, it keeps retrying with Backoff strategy from TimeoutConfig.
	//
	// For example:
	//	client.StartWithRetries("ws://localhost:8887/ws/1234")
	//
	// The function returns only when the connection has been established.
	// Incoming messages are passed automatically to the callback function, so no explicit read operation is required.
	//
	// To stop a running client, call the Stop function.
	StartWithRetries(url string)
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
	// stopped is set by Stop and cleared by Start; stopC is closed on Stop (once).
	// errMu orders sends to errC against its closing.
	stopped bool
	stopC   chan struct{}
	errMu   sync.RWMutex
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
	client.mutex.Lock()
	client.onDisconnected = handler
	client.mutex.Unlock()
}

func (client *Client) SetReconnectedHandler(handler func()) {
	client.mutex.Lock()
	client.onReconnected = handler
	client.mutex.Unlock()
}

func (client *Client) disconnectedHandler() func(err error) {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.onDisconnected
}

func (client *Client) reconnectedHandler() func() {
	client.mutex.Lock()
	defer client.mutex.Unlock()
	return client.onReconnected
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

func (client *Client) writePump(sock WebSocket) {
	ticker := time.NewTicker(client.timeoutConfig.PingPeriod)
	conn := sock.connection
	// Closure function correctly closes the current connection
	closure := func(err error) {
		ticker.Stop()
		client.cleanup(sock)
		// Invoke callback
		if handler := client.disconnectedHandler(); handler != nil {
			handler(err)
		}
	}

	for {
		select {
		case data := <-sock.outQueue:
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
		case closeErr := <-sock.closeC:
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
		case closed, ok := <-sock.forceCloseC:
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

func (client *Client) readPump(sock WebSocket) {
	conn := sock.connection
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
			sock.forceCloseC <- err
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
func (client *Client) cleanup(ws WebSocket) {
	client.setConnected(false)
	_ = ws.connection.Close()
	client.mutex.Lock()
	defer client.mutex.Unlock()
	// outQueue is never closed: a Write racing with this cleanup would panic
	// (send on closed channel). Writers select on doneC instead.
	close(ws.doneC)
	close(ws.closeC)
}

func (client *Client) handleReconnection() {
	log.Info("started automatic reconnection handler")
	delay := client.timeoutConfig.RetryBackOffWaitMinimum + time.Duration(rand.Intn(client.timeoutConfig.RetryBackOffRandomRange+1))*time.Second
	reconnectionAttempts := 1
	for {
		// Wait before reconnecting
		client.mutex.Lock()
		reconnectC := client.reconnectC
		client.mutex.Unlock()
		select {
		case <-time.After(delay):
		case <-reconnectC:
			return
		}

		log.Info("reconnecting... attempt", reconnectionAttempts)
		err := client.start(client.url.String(), true)
		if err == nil {
			// Re-connection was successful
			log.Info("reconnected successfully to server")
			if handler := client.reconnectedHandler(); handler != nil {
				handler()
			}
			return
		}
		if errors.Is(err, errClientStopped) {
			return
		}
		client.error(fmt.Errorf("reconnection failed: %w", err))

		if reconnectionAttempts < client.timeoutConfig.RetryBackOffRepeatTimes {
			// Re-connection failed, double the delay
			delay *= 2
			delay += time.Duration(rand.Intn(client.timeoutConfig.RetryBackOffRandomRange+1)) * time.Second
		}
		reconnectionAttempts += 1
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

func (client *Client) Write(data []byte) error {
	client.mutex.Lock()
	connected, outQueue, doneC := client.connected, client.webSocket.outQueue, client.webSocket.doneC
	client.mutex.Unlock()
	if !connected {
		return fmt.Errorf("client is currently not connected, cannot send data")
	}
	log.Debugf("queuing data for server")
	select {
	case outQueue <- data:
		return nil
	case <-doneC:
		return fmt.Errorf("client is currently not connected, cannot send data")
	}
}

func (client *Client) StartWithRetries(urlStr string) {
	err := client.Start(urlStr)
	if err != nil {
		log.Info("Connection error:", err)
		client.handleReconnection()
	}
}

var errClientStopped = errors.New("client was stopped")

func (client *Client) Start(urlStr string) error {
	client.mutex.Lock()
	client.stopped = false
	client.stopC = make(chan struct{})
	client.mutex.Unlock()
	return client.start(urlStr, false)
}

// start dials. A reconnection (reconnect) that finds the client stopped, which
// happens when Stop is called while the dial is in flight, closes the new
// connection again instead of leaving it behind.
func (client *Client) start(urlStr string, reconnect bool) error {
	url, err := url.Parse(urlStr)
	if err != nil {
		return err
	}
	client.url = *url

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

	sock := WebSocket{
		connection:         ws,
		id:                 id,
		outQueue:           make(chan []byte, 1),
		closeC:             make(chan websocket.CloseError, 1),
		forceCloseC:        make(chan error, 1),
		doneC:              make(chan struct{}),
		tlsConnectionState: resp.TLS,
	}
	client.mutex.Lock()
	if reconnect && client.stopped {
		client.mutex.Unlock()
		_ = ws.Close()
		return errClientStopped
	}
	client.webSocket = sock
	client.reconnectC = make(chan struct{})
	client.connected = true
	client.mutex.Unlock()
	log.Infof("connected to server as %s", id)
	// Start reader and write routine
	go client.writePump(sock)
	go client.readPump(sock)
	return nil
}

// Stop closes the connection. It may be called any number of times, also before
// Start and while a reconnection is in progress.
func (client *Client) Stop() {
	log.Infof("closing connection to server")
	client.mutex.Lock()
	client.stopped = true
	if client.stopC != nil {
		select {
		case <-client.stopC:
		default:
			close(client.stopC)
		}
	}
	if client.connected {
		client.connected = false
		// Send signal for gracefully shutting down the connection
		select {
		case client.webSocket.closeC <- websocket.CloseError{Code: websocket.CloseNormalClosure, Text: ""}:
		default:
		}
	}
	// Notify reconnection goroutine to stop (if any)
	if client.reconnectC != nil {
		select {
		case <-client.reconnectC:
		default:
			close(client.reconnectC)
		}
	}
	client.mutex.Unlock()
	client.errMu.Lock()
	if client.errC != nil {
		close(client.errC)
		client.errC = nil
	}
	client.errMu.Unlock()
	// Wait for connection to actually close
}

func (client *Client) error(err error) {
	log.Error(err)
	client.errMu.RLock()
	defer client.errMu.RUnlock()
	if client.errC == nil {
		return
	}
	client.mutex.Lock()
	stopC := client.stopC
	client.mutex.Unlock()
	select {
	case client.errC <- err:
	case <-stopC: // Stop is waiting for the write lock to close errC
	}
}

func (client *Client) Errors() <-chan error {
	client.errMu.RLock()
	if c := client.errC; c != nil {
		client.errMu.RUnlock()
		return c
	}
	client.errMu.RUnlock()
	client.errMu.Lock()
	defer client.errMu.Unlock()
	if client.errC == nil {
		client.errC = make(chan error, 1)
	}
	return client.errC
}
