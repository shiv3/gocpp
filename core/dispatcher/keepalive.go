package dispatcher

import (
	"context"
	"fmt"
	"time"
)

const defaultPongWait = 60 * time.Second

// StartKeepalive must be called immediately after Start, before any Close, so it
// joins the connection WaitGroup while other goroutines are still live.
// StartKeepalive runs fn every interval until the connection context is done.
// interval <= 0 disables it. Used by csms/cp to send OCPP Heartbeat messages.
func (c *Conn) StartKeepalive(interval time.Duration, fn func(context.Context)) {
	if interval <= 0 {
		return
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-t.C:
				fn(c.ctx)
			}
		}
	}()
}

func (c *Conn) startWebSocketPing() {
	if c.cfg.PingInterval <= 0 {
		return
	}
	pongWait := c.cfg.PongWait
	if pongWait <= 0 {
		pongWait = defaultPongWait
	}
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		t := time.NewTicker(c.cfg.PingInterval)
		defer t.Stop()
		for {
			select {
			case <-c.ctx.Done():
				return
			case <-t.C:
				pctx, cancel := context.WithTimeout(c.ctx, pongWait)
				err := c.ws.Ping(pctx)
				cancel()
				if err != nil {
					if c.ctx.Err() != nil {
						return
					}
					c.cancel(fmt.Errorf("ping: %w", err))
					return
				}
			}
		}
	}()
}
