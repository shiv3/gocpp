package dispatcher

import "sync"

// rawResult is the outcome of a Call: either a payload or an error.
type rawResult struct {
	payload []byte
	err     error
}

type pendingCall struct {
	msgID  string
	action string
	respCh chan rawResult // always 1-buffered
	cancel func() bool    // stops the call timeout timer; may be nil in tests
}

// pendingStore tracks in-flight calls. Locks guard the table only; channel
// sends always happen outside the lock (see spec §3.6).
//
// Outbound CALLs are serialized per connection by default, so almost always at
// most one call is pending: it lives in the inline one/oneID fields and the map
// is allocated only if a second call is pending at the same time (concurrent
// calls mode).
type pendingStore struct {
	mu    sync.Mutex
	oneID string
	one   *pendingCall
	more  map[string]*pendingCall
}

func newPendingStore() *pendingStore { return &pendingStore{} }

func (p *pendingStore) add(id string, pc *pendingCall) {
	p.mu.Lock()
	switch {
	case p.one == nil:
		p.oneID, p.one = id, pc
	case p.oneID == id:
		p.one = pc
	default:
		if p.more == nil {
			p.more = make(map[string]*pendingCall)
		}
		p.more[id] = pc
	}
	p.mu.Unlock()
}

// takeLocked removes and returns the entry for id, or nil.
func (p *pendingStore) takeLocked(id string) *pendingCall {
	if p.one != nil && p.oneID == id {
		pc := p.one
		p.one, p.oneID = nil, ""
		return pc
	}
	pc := p.more[id]
	if pc != nil {
		delete(p.more, id)
	}
	return pc
}

func (p *pendingStore) remove(id string) {
	p.mu.Lock()
	pc := p.takeLocked(id)
	p.mu.Unlock()
	if pc != nil && pc.cancel != nil {
		pc.cancel()
	}
}

// resolve delivers res to the waiting caller and removes the entry. Returns false
// if no such pending call exists (already resolved/removed).
func (p *pendingStore) resolve(id string, res rawResult) bool {
	p.mu.Lock()
	pc := p.takeLocked(id)
	p.mu.Unlock()
	if pc == nil {
		return false
	}
	if pc.cancel != nil {
		pc.cancel()
	}
	// respCh is 1-buffered and written at most once; nonblocking send is safe.
	select {
	case pc.respCh <- res:
	default:
	}
	return true
}

// failAll resolves every pending call with err and empties the store.
func (p *pendingStore) failAll(err error) {
	p.mu.Lock()
	toFail := make([]*pendingCall, 0, 1+len(p.more))
	if p.one != nil {
		toFail = append(toFail, p.one)
	}
	for _, pc := range p.more {
		toFail = append(toFail, pc)
	}
	p.one, p.oneID, p.more = nil, "", nil
	p.mu.Unlock()
	for _, pc := range toFail {
		if pc.cancel != nil {
			pc.cancel()
		}
		select {
		case pc.respCh <- rawResult{err: err}:
		default:
		}
	}
}

func (p *pendingStore) len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	n := len(p.more)
	if p.one != nil {
		n++
	}
	return n
}
