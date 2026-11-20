// Package common holds the library-independent parts of the parity test
// servers: the event log, the canned-response rules and the control HTTP API.
// It deliberately has no OCPP library dependency.
package common

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
	"time"
)

// Event kinds.
const (
	KindConnect      = "connect"
	KindDisconnect   = "disconnect"
	KindRequest      = "request"
	KindResponseSent = "response_sent"
	KindPanicPending = "handler_panic_pending"
	KindError        = "error"
	KindLog          = "log"
)

// Disconnect causes.
const (
	CauseReplaced     = "replaced"
	CauseServerClosed = "server_closed"
	CausePeerClosed   = "peer_closed"
	CauseReadError    = "read_error"
	CauseTimeout      = "timeout"
	CauseUnknown      = "unknown"
)

// Event is one normalized, library-independent observation.
type Event struct {
	Seq     int64           `json:"seq"`
	Time    string          `json:"time"`
	Kind    string          `json:"kind"`
	CpID    string          `json:"cpId,omitempty"`
	Action  string          `json:"action,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Detail  string          `json:"detail,omitempty"`
	Cause   string          `json:"cause,omitempty"`
	Level   string          `json:"level,omitempty"`
	Msg     string          `json:"msg,omitempty"`
	Attrs   map[string]any  `json:"attrs,omitempty"`
}

// Recorder keeps events in memory and optionally appends them to a JSONL file.
// It also tracks the set of connected charge point ids.
type Recorder struct {
	mu     sync.Mutex
	seq    int64
	events []Event
	file   *os.File
	conns  map[string]int // id -> number of live registrations
}

// NewRecorder opens path for appending (empty path: memory only).
func NewRecorder(path string) (*Recorder, error) {
	r := &Recorder{conns: map[string]int{}, events: []Event{}}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, err
		}
		r.file = f
	}
	return r, nil
}

// Add stamps seq/time on e, stores it and appends it to the file. The file
// write is unbuffered so the event survives a process crash right after.
func (r *Recorder) Add(e Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addLocked(e)
}

func (r *Recorder) addLocked(e Event) {
	r.seq++
	e.Seq = r.seq
	e.Time = time.Now().UTC().Format(time.RFC3339Nano)
	r.events = append(r.events, e)
	if r.file != nil {
		if b, err := json.Marshal(e); err == nil {
			_, _ = r.file.Write(append(b, '\n'))
		}
	}
}

// Events returns a copy of all events so far.
func (r *Recorder) Events() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Event{}, r.events...)
}

// Reset clears the in-memory events (the JSONL file is left alone) and the
// sequence counter. Connection tracking is unaffected.
func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = []Event{}
	r.seq = 0
}

// Connect records a connect event and marks the id connected.
func (r *Recorder) Connect(cpID, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conns[cpID]++
	r.addLocked(Event{Kind: KindConnect, CpID: cpID, Detail: detail})
}

// Disconnect records a disconnect event and marks one registration gone.
func (r *Recorder) Disconnect(cpID, cause, detail string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.conns[cpID] > 1 {
		r.conns[cpID]--
	} else {
		delete(r.conns, cpID)
	}
	if cause == "" {
		cause = CauseUnknown
	}
	r.addLocked(Event{Kind: KindDisconnect, CpID: cpID, Cause: cause, Detail: detail})
}

// Connections returns the sorted ids currently connected.
func (r *Recorder) Connections() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.conns))
	for id := range r.conns {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// IsConnected reports whether id is currently connected.
func (r *Recorder) IsConnected(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.conns[id] > 0
}

func toJSON(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		b, _ = json.Marshal(map[string]string{"marshalError": err.Error()})
	}
	return b
}

// Request records that a handler was invoked with the decoded request v.
func (r *Recorder) Request(cpID, action string, v any) {
	r.Add(Event{Kind: KindRequest, CpID: cpID, Action: action, Payload: toJSON(v)})
}

// Response records the response a handler produced (or its error text).
func (r *Recorder) Response(cpID, action string, v any, err error) {
	e := Event{Kind: KindResponseSent, CpID: cpID, Action: action}
	if err != nil {
		e.Detail = "error: " + err.Error()
	} else {
		e.Payload = toJSON(v)
	}
	r.Add(e)
}

// PanicPending records that the handler is about to panic deliberately.
func (r *Recorder) PanicPending(cpID, action string) {
	r.Add(Event{Kind: KindPanicPending, CpID: cpID, Action: action, Detail: "about to panic"})
}

// Error records a library-level error.
func (r *Recorder) Error(cpID, detail string) {
	r.Add(Event{Kind: KindError, CpID: cpID, Detail: detail})
}
