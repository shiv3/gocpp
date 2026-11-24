package csms

import (
	"context"
	"sort"
	"sync"

	"github.com/shiv3/gocpp/core/ocppj"
)

// connHandler is a registered inbound CALL handler with its payload decoding
// and response encoding, typed on *Conn instead of *dispatcher.Conn.
type connHandler func(ctx context.Context, c *Conn, payload []byte) ([]byte, error)

// connSendHandler is connHandler for an inbound SEND.
type connSendHandler func(ctx context.Context, c *Conn, payload []byte) error

// handlerTable holds the handlers registered through On and OnSend, keyed by
// action. The dispatcher's HandlerRegistry holds the same handlers wrapped for
// a dispatcher.Conn; this table lets a server that receives (action, payload)
// from a gateway link run them against a Conn that has no WebSocket of its own.
type handlerTable struct {
	mu    sync.RWMutex
	calls map[string]connHandler
	sends map[string]connSendHandler

	vcalls map[ocppj.Version]map[string]connHandler
	vsends map[ocppj.Version]map[string]connSendHandler
}

func (t *handlerTable) setCallVersion(v ocppj.Version, action string, h connHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vcalls == nil {
		t.vcalls = make(map[ocppj.Version]map[string]connHandler)
	}
	if t.vcalls[v] == nil {
		t.vcalls[v] = make(map[string]connHandler)
	}
	t.vcalls[v][action] = h
}

func (t *handlerTable) setSendVersion(v ocppj.Version, action string, h connSendHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.vsends == nil {
		t.vsends = make(map[ocppj.Version]map[string]connSendHandler)
	}
	if t.vsends[v] == nil {
		t.vsends[v] = make(map[string]connSendHandler)
	}
	t.vsends[v][action] = h
}

// callFor resolves a call handler for a session of version v: the (v, action)
// handler first, then the version-agnostic one.
func (t *handlerTable) callFor(v ocppj.Version, action string) (connHandler, bool) {
	t.mu.RLock()
	h, ok := t.vcalls[v][action]
	t.mu.RUnlock()
	if ok {
		return h, true
	}
	return t.call(action)
}

func (t *handlerTable) sendFor(v ocppj.Version, action string) (connSendHandler, bool) {
	t.mu.RLock()
	h, ok := t.vsends[v][action]
	t.mu.RUnlock()
	if ok {
		return h, true
	}
	return t.send(action)
}

func (t *handlerTable) setCall(action string, h connHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.calls == nil {
		t.calls = make(map[string]connHandler)
	}
	t.calls[action] = h
}

func (t *handlerTable) setSend(action string, h connSendHandler) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.sends == nil {
		t.sends = make(map[string]connSendHandler)
	}
	t.sends[action] = h
}

func (t *handlerTable) call(action string) (connHandler, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	h, ok := t.calls[action]
	return h, ok
}

func (t *handlerTable) send(action string) (connSendHandler, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	h, ok := t.sends[action]
	return h, ok
}

// actions lists the actions with a registered call or send handler, sorted.
func (t *handlerTable) actions() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	seen := make(map[string]struct{}, len(t.calls)+len(t.sends))
	for a := range t.calls {
		seen[a] = struct{}{}
	}
	for a := range t.sends {
		seen[a] = struct{}{}
	}
	for _, m := range t.vcalls {
		for a := range m {
			seen[a] = struct{}{}
		}
	}
	for _, m := range t.vsends {
		for a := range m {
			seen[a] = struct{}{}
		}
	}
	out := make([]string, 0, len(seen))
	for a := range seen {
		out = append(out, a)
	}
	sort.Strings(out)
	return out
}

// has reports whether any handler, of any version, is registered for action.
func (t *handlerTable) has(action string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if _, ok := t.calls[action]; ok {
		return true
	}
	if _, ok := t.sends[action]; ok {
		return true
	}
	for _, m := range t.vcalls {
		if _, ok := m[action]; ok {
			return true
		}
	}
	for _, m := range t.vsends {
		if _, ok := m[action]; ok {
			return true
		}
	}
	return false
}
