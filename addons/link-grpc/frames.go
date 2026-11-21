package linkgrpc

import (
	"errors"
	"sync"
)

// errStreamBroken is what pending requests get when their stream ends.
var errStreamBroken = errors.New("linkgrpc: stream broken")

// pendingSet correlates request ids with their answers on one stream.
type pendingSet[T any] struct {
	mu     sync.Mutex
	next   uint64
	m      map[uint64]chan T
	closed bool
}

func newPendingSet[T any]() *pendingSet[T] { return &pendingSet[T]{m: make(map[uint64]chan T)} }

// add registers a new request and returns its id and answer channel; ok is
// false once the stream is closed.
func (p *pendingSet[T]) add() (id uint64, ch chan T, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return 0, nil, false
	}
	p.next++
	ch = make(chan T, 1)
	p.m[p.next] = ch
	return p.next, ch, true
}

func (p *pendingSet[T]) resolve(id uint64, v T) bool {
	p.mu.Lock()
	ch, ok := p.m[id]
	delete(p.m, id)
	p.mu.Unlock()
	if ok {
		ch <- v
	}
	return ok
}

func (p *pendingSet[T]) drop(id uint64) {
	p.mu.Lock()
	delete(p.m, id)
	p.mu.Unlock()
}

// close fails every pending request: the channels are closed, which a waiter
// sees as a receive with ok == false.
func (p *pendingSet[T]) close() {
	p.mu.Lock()
	p.closed = true
	for id, ch := range p.m {
		delete(p.m, id)
		close(ch)
	}
	p.mu.Unlock()
}
