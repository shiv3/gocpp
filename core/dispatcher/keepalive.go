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

// startReadWatchdog cancels the connection if no inbound frame (data, ping, or
// pong) arrives within cfg.ReadTimeout. ReadTimeout <= 0 disables it.
//
// It is a single time.AfterFunc timer, not a goroutine: inbound frames only
// store a timestamp (NoteActivity). When the timer fires it compares the idle
// time with the timeout and either cancels the connection or re-arms for the
// remaining time, so the cost per frame is two atomic stores.
func (c *Conn) startReadWatchdog() {
	rt := c.cfg.ReadTimeout
	if rt <= 0 {
		return
	}
	c.tmu.Lock()
	defer c.tmu.Unlock()
	c.wdTimer = time.AfterFunc(rt, func() {
		c.tmu.Lock()
		defer c.tmu.Unlock()
		if c.timersStopped || c.ctx.Err() != nil {
			return
		}
		idle := time.Since(c.monoBase) - time.Duration(c.recvMono.Load())
		if idle >= rt {
			c.cancel(fmt.Errorf("read idle timeout after %s", rt))
			return
		}
		c.wdTimer.Reset(rt - idle)
	})
}

// startWebSocketPing sends a WebSocket ping every cfg.PingInterval (counted from
// the end of the previous ping). A time.AfterFunc callback runs each ping, so a
// goroutine exists only while a pong is awaited; a ping that gets no pong within
// PongWait, or fails, cancels the connection.
func (c *Conn) startWebSocketPing() {
	interval := c.cfg.PingInterval
	if interval <= 0 {
		return
	}
	pongWait := c.cfg.PongWait
	if pongWait <= 0 {
		pongWait = defaultPongWait
	}
	c.tmu.Lock()
	defer c.tmu.Unlock()
	c.pingTimer = time.AfterFunc(interval, func() {
		// Register as in flight so teardown can wait for this callback.
		c.tmu.Lock()
		if c.timersStopped || c.ctx.Err() != nil {
			c.tmu.Unlock()
			return
		}
		c.timerWG.Add(1)
		c.tmu.Unlock()
		defer c.timerWG.Done()

		pctx, cancel := context.WithTimeout(c.ctx, pongWait)
		pingStart := time.Now()
		c.lastSent.Store(pingStart.UnixNano())
		err := c.ws.Ping(pctx)
		cancel()
		if err != nil {
			if c.ctx.Err() == nil {
				c.cancel(fmt.Errorf("ping: %w", err))
			}
			return
		}
		if m, ok := c.cfg.Metrics.(connectionMetricsHook); ok {
			m.PingLatency(string(c.version), time.Since(pingStart))
		}
		c.tmu.Lock()
		defer c.tmu.Unlock()
		if !c.timersStopped && c.ctx.Err() == nil {
			c.pingTimer.Reset(interval)
		}
	})
}

// stopTimers stops the keepalive timers and waits for an in-flight ping callback
// (which ends promptly: its context is the connection's). Called once the
// connection goroutines have exited, before done is closed.
func (c *Conn) stopTimers() {
	c.tmu.Lock()
	c.timersStopped = true
	if c.wdTimer != nil {
		c.wdTimer.Stop()
	}
	if c.pingTimer != nil {
		c.pingTimer.Stop()
	}
	c.tmu.Unlock()
	c.timerWG.Wait()
}
