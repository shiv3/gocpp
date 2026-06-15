// Package cp implements the OCPP Charge Point (client) side.
package cp

import (
	"context"
	"errors"
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

	mu   sync.RWMutex
	conn *dispatcher.Conn
}

// NewClient creates a charge point client. Call Connect or Run to establish.
func NewClient(cpID, csmsURL string, opts ...Option) *Client {
	cfg := defaultClientConfig()
	for _, o := range opts {
		o.apply(&cfg)
	}
	return &Client{
		id:  cpID,
		url: csmsURL,
		cfg: cfg,
		reg: dispatcher.NewHandlerRegistry(),
	}
}

// Connect establishes a single connection (no auto-reconnect).
func (c *Client) Connect(ctx context.Context) error {
	wsConn, _, err := websocket.Dial(ctx, c.url, &websocket.DialOptions{
		Subprotocols: c.cfg.subProtocols,
	})
	if err != nil {
		return err
	}
	if wsConn.Subprotocol() == "" {
		wsConn.Close(websocket.StatusProtocolError, "no common subprotocol")
		return ocppj.ErrVersionMismatch
	}
	ws := transport.NewCoderWS(wsConn)
	dconn := dispatcher.NewConn(c.id, ws, c.cfg.dispatcher, c.reg)
	dconn.Start(context.Background())

	c.mu.Lock()
	c.conn = dconn
	c.mu.Unlock()
	return nil
}

// IsConnected reports whether a live connection exists.
func (c *Client) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil && c.conn.Context().Err() == nil
}

// Close tears down the current connection.
func (c *Client) Close() {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()
	if conn != nil {
		_ = conn.Close(nil)
	}
}

func (c *Client) current() *dispatcher.Conn {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn
}

// Run connects and maintains the connection with exponential backoff until ctx is
// cancelled. In-flight calls fail on disconnect (OCPP has no idempotency guarantee).
func (c *Client) Run(ctx context.Context) error {
	bo := backoff.NewExponentialBackOff()
	for {
		err := c.connectAndServe(ctx)
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

func (c *Client) connectAndServe(ctx context.Context) error {
	if err := c.Connect(ctx); err != nil {
		return err
	}
	conn := c.current()
	select {
	case <-conn.Context().Done():
		_ = conn.Close(nil)
		return conn.Context().Err()
	case <-ctx.Done():
		_ = conn.Close(nil)
		return context.Canceled
	}
}
