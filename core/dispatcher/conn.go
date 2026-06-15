package dispatcher

import (
	"context"
	"fmt"
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

	out chan outbound
	in  chan ocppj.Frame

	pending *pendingStore
	reg     *HandlerRegistry
	sem     *semaphore.Weighted

	ctx     context.Context
	cancel  context.CancelCauseFunc
	closeWS func()
	done    chan struct{}
	wg      sync.WaitGroup

	cfg Config
}

type outbound struct {
	payload []byte
	sentCh  chan error // 1-buffered
}

// NewConn creates a connection. Call Start to launch its goroutines.
func NewConn(id string, ws transport.WS, cfg Config, reg *HandlerRegistry) *Conn {
	c := &Conn{
		id:      id,
		ws:      ws,
		version: subprotocolToVersion(ws.Subprotocol()),
		out:     make(chan outbound, cfg.OutboundQueueSize),
		in:      make(chan ocppj.Frame, cfg.OutboundQueueSize),
		pending: newPendingStore(),
		reg:     reg,
		sem:     semaphore.NewWeighted(cfg.MaxConcurrentHandlers),
		done:    make(chan struct{}),
		cfg:     cfg,
	}
	c.closeWS = sync.OnceFunc(func() { _ = ws.Close(transport.StatusNormalClosure, "closed") })
	return c
}

// ID returns the charge point identifier.
func (c *Conn) ID() string { return c.id }

// Version returns the negotiated OCPP version.
func (c *Conn) Version() ocppj.Version { return c.version }

// Context returns the connection lifecycle context.
func (c *Conn) Context() context.Context { return c.ctx }

// Start launches the connection goroutines bound to parent.
func (c *Conn) Start(parent context.Context) {
	c.ctx, c.cancel = context.WithCancelCause(parent)
	c.cfg.Metrics.ConnectionOpened()

	c.wg.Add(3)
	go c.reader()
	go c.writer()
	go c.dispatch()

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
		frame, err := ocppj.Parse(msg)
		if err != nil {
			c.cfg.Logger.WarnContext(c.ctx, "ocpp parse error",
				"cp_id", c.id, "err", err)
			continue
		}
		switch frame.Type {
		case ocppj.CallResult:
			c.pending.resolve(frame.MsgID, rawResult{payload: frame.Payload})
		case ocppj.MessageTypeCallError:
			ce := &ocppj.CallError{Code: ocppj.ErrorCode(frame.ErrCode), Description: frame.ErrDesc}
			c.pending.resolve(frame.MsgID, rawResult{err: ce})
		case ocppj.Call:
			select {
			case c.in <- frame:
			case <-c.ctx.Done():
				return
			}
		}
	}
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
