// Package msglog is the message log of gocpp-csms serve mode: every OCPP-J
// frame in both directions (CALL, CALLRESULT, CALLERROR, CALLRESULTERROR and
// SEND), taken from the server's frame observer, so the order is the wire
// order of each connection without serialising the handlers.
//
// Entries go to an in-memory ring buffer served by GET /messages and the
// server-sent event stream GET /messages/stream, and optionally to a JSONL
// file and (--record-frames) to the stdout recorder.
package msglog

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
)

// Directions of an Entry.
const (
	// In is a frame the charge point sent to the CSMS.
	In = "in"
	// Out is a frame the CSMS sent to the charge point.
	Out = "out"
)

// Entry is one logged frame.
type Entry struct {
	Seq       int64     `json:"seq"`
	Time      time.Time `json:"time"`
	CPID      string    `json:"cpId"`
	Version   string    `json:"version,omitempty"`
	Direction string    `json:"direction"`
	// Type is CALL, CALLRESULT, CALLERROR, CALLRESULTERROR or SEND; empty
	// for a frame that could not be parsed (Raw holds it then).
	Type  string `json:"type,omitempty"`
	MsgID string `json:"msgId,omitempty"`
	// Action is the CALL's action; for a response it is the action of the
	// CALL it answers when that CALL was seen.
	Action           string          `json:"action,omitempty"`
	Payload          json.RawMessage `json:"payload,omitempty"`
	ErrorCode        string          `json:"errorCode,omitempty"`
	ErrorDescription string          `json:"errorDescription,omitempty"`
	ErrorDetails     json.RawMessage `json:"errorDetails,omitempty"`
	// Raw is the frame text when it is not a well-formed OCPP-J array.
	Raw string `json:"raw,omitempty"`
	// WriteError is set for an outbound frame that failed to be written.
	WriteError string `json:"writeError,omitempty"`
}

// Sink receives every entry, in log order, under the log's lock: it must be
// fast and must not call back into the Log.
type Sink func(Entry)

// maxPendingPerCP bounds the CALLs awaiting an answer remembered per charge
// point (for the action of responses).
const maxPendingPerCP = 1024

// Log assigns sequence numbers and fans frames out to the sinks.
type Log struct {
	mu      sync.Mutex
	seq     int64
	sinks   []Sink
	pending map[string]map[pendingKey]string // cpId -> call -> action
	now     func() time.Time
}

type pendingKey struct {
	callDir string
	msgID   string
}

// New returns a Log feeding sinks.
func New(sinks ...Sink) *Log {
	return &Log{sinks: sinks, pending: map[string]map[pendingKey]string{}, now: time.Now}
}

// AddSink adds a sink (before frames flow).
func (l *Log) AddSink(s Sink) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sinks = append(l.sinks, s)
}

// Observe is the frame observer feeding the log.
func (l *Log) Observe(ev observability.FrameEvent) {
	e := parse(ev)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.correlate(&e)
	l.seq++
	e.Seq = l.seq
	if e.Time.IsZero() {
		e.Time = l.now().UTC()
	}
	for _, s := range l.sinks {
		s(e)
	}
}

// Forget drops the unanswered CALLs remembered for cpID (on disconnect).
func (l *Log) Forget(cpID string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.pending, cpID)
}

func (l *Log) correlate(e *Entry) {
	switch e.Type {
	case "CALL":
		if e.MsgID == "" {
			return
		}
		m := l.pending[e.CPID]
		if m == nil {
			m = map[pendingKey]string{}
			l.pending[e.CPID] = m
		}
		if len(m) >= maxPendingPerCP {
			clear(m)
		}
		m[pendingKey{e.Direction, e.MsgID}] = e.Action
	case "CALLRESULT", "CALLERROR", "CALLRESULTERROR":
		m := l.pending[e.CPID]
		k := pendingKey{opposite(e.Direction), e.MsgID}
		if a, ok := m[k]; ok {
			if e.Action == "" {
				e.Action = a
			}
			delete(m, k)
		}
	}
}

func opposite(dir string) string {
	if dir == In {
		return Out
	}
	return In
}

func typeName(t ocppj.MessageType) string {
	switch t {
	case ocppj.Call:
		return "CALL"
	case ocppj.CallResult:
		return "CALLRESULT"
	case ocppj.MessageTypeCallError:
		return "CALLERROR"
	case ocppj.MessageTypeCallResultError:
		return "CALLRESULTERROR"
	case ocppj.Send:
		return "SEND"
	}
	return ""
}

// parse turns a raw frame into an Entry (without seq). The raw bytes are
// copied: the event's buffer belongs to the connection.
func parse(ev observability.FrameEvent) Entry {
	e := Entry{
		Time:      ev.Time.UTC(),
		CPID:      ev.CPID,
		Version:   string(ev.Version),
		Direction: string(ev.Direction),
		MsgID:     ev.MsgID,
		Action:    ev.Action,
	}
	if ev.Direction == observability.FrameIn {
		e.Direction = In
	} else {
		e.Direction = Out
	}
	if ev.Err != nil {
		e.WriteError = ev.Err.Error()
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(ev.Raw, &arr); err != nil || len(arr) < 3 {
		e.Type = typeName(ev.Type)
		e.Raw = string(ev.Raw)
		return e
	}
	var typ int
	if json.Unmarshal(arr[0], &typ) != nil {
		e.Raw = string(ev.Raw)
		return e
	}
	e.Type = typeName(ocppj.MessageType(typ))
	if e.Type == "" {
		e.Raw = string(ev.Raw)
		return e
	}
	var id string
	if json.Unmarshal(arr[1], &id) == nil {
		e.MsgID = id
	}
	switch ocppj.MessageType(typ) {
	case ocppj.Call, ocppj.Send:
		var action string
		if json.Unmarshal(arr[2], &action) == nil {
			e.Action = action
		}
		if len(arr) > 3 {
			e.Payload = redact(e.Action, compact(arr[3]))
		}
	case ocppj.CallResult:
		e.Payload = compact(arr[2])
	case ocppj.MessageTypeCallError, ocppj.MessageTypeCallResultError:
		_ = json.Unmarshal(arr[2], &e.ErrorCode)
		if len(arr) > 3 {
			_ = json.Unmarshal(arr[3], &e.ErrorDescription)
		}
		if len(arr) > 4 {
			e.ErrorDetails = compact(arr[4])
		}
	}
	return e
}

func compact(b json.RawMessage) json.RawMessage {
	var buf bytes.Buffer
	if json.Compact(&buf, b) != nil {
		return append(json.RawMessage(nil), b...)
	}
	return buf.Bytes()
}

// Redacted replaces a secret value in a logged payload.
const Redacted = "***"

// redact hides the credentials a CSMS sends to a charge point: the 1.6
// AuthorizationKey configuration key (ChangeConfiguration) and the 2.x
// BasicAuthPassword variable (SetVariables). Other payloads are unchanged.
func redact(action string, payload json.RawMessage) json.RawMessage {
	switch action {
	case "ChangeConfiguration":
		var m map[string]any
		if json.Unmarshal(payload, &m) != nil {
			return payload
		}
		if k, _ := m["key"].(string); strings.EqualFold(k, "AuthorizationKey") {
			if _, ok := m["value"]; ok {
				m["value"] = Redacted
				return marshal(m, payload)
			}
		}
	case "SetVariables":
		var m map[string]any
		if json.Unmarshal(payload, &m) != nil {
			return payload
		}
		data, _ := m["setVariableData"].([]any)
		changed := false
		for _, d := range data {
			dm, _ := d.(map[string]any)
			v, _ := dm["variable"].(map[string]any)
			if name, _ := v["name"].(string); strings.EqualFold(name, "BasicAuthPassword") {
				if _, ok := dm["attributeValue"]; ok {
					dm["attributeValue"] = Redacted
					changed = true
				}
			}
		}
		if changed {
			return marshal(m, payload)
		}
	}
	return payload
}

func marshal(v any, fallback json.RawMessage) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return fallback
	}
	return b
}
