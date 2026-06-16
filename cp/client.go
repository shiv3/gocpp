// Package cp implements the OCPP Charge Point (client) side.
package cp

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/transport"
)

// Client is an OCPP charge point.
type Client struct {
	id  string
	url string
	cfg clientConfig
	reg *dispatcher.HandlerRegistry

	mu    sync.RWMutex
	conn  *dispatcher.Conn
	queue *outboundQueue
}

// NewClient creates a charge point client. Call Connect or Run to establish.
func NewClient(cpID, csmsURL string, opts ...Option) *Client {
	cfg := defaultClientConfig()
	for _, o := range opts {
		o.apply(&cfg)
	}
	if cfg.registry != nil && cfg.dispatcher.SchemaMode != dispatcher.SchemaModeOff {
		cfg.dispatcher.SchemaValidate = func(version ocppj.Version, action, kind string, payload []byte) error {
			v, ok := cfg.registry.Lookup(string(version), action, kind)
			if !ok {
				return nil
			}
			return v.Validate(payload)
		}
	}
	client := &Client{
		id:  cpID,
		url: csmsURL,
		cfg: cfg,
		reg: dispatcher.NewHandlerRegistry(),
	}
	if cfg.offlineQueueCapacity > 0 {
		client.queue = newOutboundQueue(cfg.offlineQueueCapacity, cfg.dispatcher.CallTimeout, cfg.retryInFlight)
	}
	return client
}

// Connect establishes a single connection (no auto-reconnect).
func (c *Client) Connect(ctx context.Context) error {
	return c.connect(ctx, false, true)
}

func (c *Client) connect(ctx context.Context, reconnected, watch bool) error {
	wsConn, _, err := websocket.Dial(ctx, c.url, c.cfg.dialOptions())
	if err != nil {
		return err
	}
	if wsConn.Subprotocol() == "" {
		_ = wsConn.Close(websocket.StatusProtocolError, "no common subprotocol")
		return ocppj.ErrVersionMismatch
	}
	ws := transport.NewCoderWS(wsConn)
	dconn := dispatcher.NewConn(c.id, ws, c.cfg.dispatcher, c.reg)
	dconn.Start(context.Background())
	c.startHeartbeat(dconn)
	c.handleConnected(dconn, reconnected)
	if watch {
		go c.watchConn(dconn)
	}
	return nil
}

func (c *Client) startHeartbeat(dconn *dispatcher.Conn) {
	dconn.StartKeepalive(c.cfg.heartbeatInterval, func(ctx context.Context) {
		if _, err := dispatcher.DoCall(ctx, dconn, "Heartbeat", []byte("{}")); err != nil && ctx.Err() == nil {
			c.cfg.dispatcher.Logger.WarnContext(ctx, "heartbeat failed", "err", err)
		}
	})
}

func (c *clientConfig) dialOptions() *websocket.DialOptions {
	opts := &websocket.DialOptions{
		Subprotocols: c.subProtocols,
	}
	if c.httpHeader != nil {
		opts.HTTPHeader = c.httpHeader.Clone()
	}
	if c.basicAuthUser != "" || c.basicAuthPass != "" {
		if opts.HTTPHeader == nil {
			opts.HTTPHeader = make(http.Header)
		}
		token := base64.StdEncoding.EncodeToString([]byte(c.basicAuthUser + ":" + c.basicAuthPass))
		opts.HTTPHeader.Set("Authorization", "Basic "+token)
	}
	if c.tlsConfig != nil {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: c.tlsConfig},
		}
	}
	return opts
}

// IsConnected reports whether a live connection exists.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && c.conn.Context().Err() == nil
}

// NegotiatedProtocol returns the subprotocol chosen during the handshake.
func (c *Client) NegotiatedProtocol() string {
	conn := c.current()
	if conn == nil {
		return ""
	}
	return conn.Subprotocol()
}

// Close tears down the current connection.
func (c *Client) Close() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if c.queue != nil {
		c.queue.setConn(nil)
	}
	if conn != nil {
		_ = conn.Close(nil)
	}
	if c.queue != nil {
		c.queue.close(ocppj.ErrConnClosed)
	}
}

func (c *Client) current() *dispatcher.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

func (c *Client) publishConn(conn *dispatcher.Conn) {
	c.mu.Lock()
	c.conn = conn
	c.mu.Unlock()
	if c.queue != nil {
		c.queue.setConn(conn)
	}
}

func (c *Client) handleConnected(conn *dispatcher.Conn, reconnected bool) {
	c.publishConn(conn)
	if c.cfg.onConnect != nil {
		c.cfg.onConnect()
	}
	if reconnected && c.cfg.onReconnect != nil {
		c.cfg.onReconnect()
	}
}

func (c *Client) handleDisconnected(conn *dispatcher.Conn, err error) {
	if err == nil {
		err = ocppj.ErrConnClosed
	}
	c.clearConn(conn)
	if c.cfg.onDisconnect != nil {
		c.cfg.onDisconnect(err)
	}
}

func (c *Client) watchConn(conn *dispatcher.Conn) {
	<-conn.Context().Done()
	c.handleDisconnected(conn, context.Cause(conn.Context()))
}

func (c *Client) clearConn(conn *dispatcher.Conn) {
	c.mu.Lock()
	if c.conn == conn {
		c.conn = nil
		if c.queue != nil {
			c.queue.setConn(nil)
		}
	}
	c.mu.Unlock()
}

func (c *Client) queueLen() int {
	if c.queue == nil {
		return 0
	}
	return c.queue.len()
}

// Run connects and maintains the connection with exponential backoff until ctx is
// cancelled. In-flight calls fail on disconnect (OCPP has no idempotency guarantee).
func (c *Client) Run(ctx context.Context) error {
	if c.queue != nil {
		defer c.queue.close(context.Canceled)
	}
	bo := backoff.NewExponentialBackOff()
	reconnect := false
	for {
		connected, err := c.connectAndServe(ctx, reconnect)
		if connected {
			reconnect = true
		}
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			return nil
		}
		delay := bo.NextBackOff()
		if delay == backoff.Stop {
			return err
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil
		}
	}
}

func (c *Client) connectAndServe(ctx context.Context, reconnected bool) (bool, error) {
	if err := c.connect(ctx, reconnected, false); err != nil {
		return false, err
	}
	conn := c.current()
	select {
	case <-conn.Context().Done():
		err := context.Cause(conn.Context())
		_ = conn.Close(nil)
		c.handleDisconnected(conn, err)
		return true, err
	case <-ctx.Done():
		_ = conn.Close(nil)
		c.handleDisconnected(conn, context.Canceled)
		return true, context.Canceled
	}
}
