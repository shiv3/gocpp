package dispatcher

import (
	"context"
	"time"
)

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
