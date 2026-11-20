package dispatcher

import (
	"sync"

	"github.com/shiv3/gocpp/core/ocppj"
)

// handlerPoolRecycle is the number of tasks a pool worker runs before it is
// replaced by a fresh goroutine, so a stack grown by one unusual message can
// shrink again.
var handlerPoolRecycle = 1 << 16

// HandlerPool is an optional process-wide set of resident goroutines that run
// inbound handlers, so that a handler does not start on a fresh 2 KB stack and
// grow it (copying it several times) for every message. Share one pool between
// all connections via Config.HandlerPool.
//
// It is a hand-off, never a queue: a message goes to a worker only if one is
// idle at that moment, and otherwise runs on a new goroutine exactly as without
// a pool. A slow handler can therefore never delay another connection's handlers
// (no shared queue, no global wait), and the pool adds no limit on concurrency.
type HandlerPool struct {
	tasks chan poolTask
	quit  chan struct{}
	once  sync.Once
	wg    sync.WaitGroup
}

type poolTask struct {
	c *Conn
	f ocppj.Frame
}

// NewHandlerPool starts n resident workers (n < 1 means 1). Call Close when no
// connection uses it any more.
func NewHandlerPool(n int) *HandlerPool {
	p := &HandlerPool{tasks: make(chan poolTask), quit: make(chan struct{})}
	for i := 0; i < max(n, 1); i++ {
		p.wg.Add(1)
		go p.worker()
	}
	return p
}

// Close stops the workers after the handlers they are running return. Messages
// dispatched afterwards simply run on new goroutines.
func (p *HandlerPool) Close() {
	p.once.Do(func() { close(p.quit) })
	p.wg.Wait()
}

func (p *HandlerPool) worker() {
	defer p.wg.Done()
	for n := 0; n < handlerPoolRecycle; n++ {
		select {
		case t := <-p.tasks:
			t.c.handlerLoop(t.f)
		case <-p.quit:
			return
		}
	}
	// Replace this worker with one on a fresh stack.
	select {
	case <-p.quit:
	default:
		p.wg.Add(1)
		go p.worker()
	}
}

// try hands the frame to an idle worker. It never blocks.
func (p *HandlerPool) try(c *Conn, f ocppj.Frame) bool {
	select {
	case p.tasks <- poolTask{c, f}:
		return true
	default:
		return false
	}
}

// startHandler runs c.handlerLoop(f) on a pool worker when one is idle and on a
// new goroutine otherwise. The caller has already counted it in c.running.
func (c *Conn) startHandler(f ocppj.Frame) {
	if p := c.cfg.HandlerPool; p != nil && p.try(c, f) {
		return
	}
	go c.handlerLoop(f)
}
