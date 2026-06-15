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
	cancel func() bool    // context.AfterFunc stop; may be nil in tests
}

// pendingStore tracks in-flight calls. Locks guard map mutation only; channel
// sends always happen outside the lock (see spec §3.6).
type pendingStore struct {
	mu      sync.RWMutex
	entries map[string]*pendingCall
}

func newPendingStore() *pendingStore {
	return &pendingStore{entries: make(map[string]*pendingCall)}
}

func (p *pendingStore) add(id string, pc *pendingCall) {
	p.mu.Lock()
	p.entries[id] = pc
	p.mu.Unlock()
}

func (p *pendingStore) remove(id string) {
	p.mu.Lock()
	pc := p.entries[id]
	delete(p.entries, id)
	p.mu.Unlock()
	if pc != nil && pc.cancel != nil {
		pc.cancel()
	}
}

// resolve delivers res to the waiting caller and removes the entry. Returns false
// if no such pending call exists (already resolved/removed).
func (p *pendingStore) resolve(id string, res rawResult) bool {
	p.mu.Lock()
	pc, ok := p.entries[id]
	if ok {
		delete(p.entries, id)
	}
	p.mu.Unlock()
	if !ok {
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
	toFail := make([]*pendingCall, 0, len(p.entries))
	for _, pc := range p.entries {
		toFail = append(toFail, pc)
	}
	p.entries = make(map[string]*pendingCall)
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
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.entries)
}
