// Package recorder writes the NDJSON frame log of gocpp-csms: one line per
// charge-point CALL the CSMS handled,
//
//	{"seq":N,"cpId":"..","action":"..","payload":{...}}
//
// with seq counting from 1 across all connections, and the ports sentinel line
// of the legacy e2e mode. The format is part of the frozen legacy contract (see
// legacy_contract_test.go); serve mode writes the same lines.
package recorder

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sync"
)

// Recorder serialises frame lines onto one writer, flushing after each line.
type Recorder struct {
	mu  sync.Mutex
	seq int64
	enc *json.Encoder
	out *bufio.Writer
}

type recordedFrame struct {
	Seq     int64  `json:"seq"`
	CPID    string `json:"cpId"`
	Action  string `json:"action"`
	Payload any    `json:"payload"`
}

// New returns a Recorder writing to out.
func New(out *bufio.Writer) *Recorder {
	return &Recorder{
		enc: json.NewEncoder(out),
		out: out,
	}
}

// Record writes one frame line. payload is encoded as JSON (a json.RawMessage
// is written compacted, as is).
func (r *Recorder) Record(cpID, action string, payload any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	if err := r.enc.Encode(recordedFrame{
		Seq:     r.seq,
		CPID:    cpID,
		Action:  action,
		Payload: payload,
	}); err != nil {
		return err
	}
	return r.out.Flush()
}

// RecordLine writes one line built by line from the next seq number (a
// superset of the frame line, for example). It shares the seq counter and
// the lock with Record.
func (r *Recorder) RecordLine(line func(seq int64) any) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.seq++
	if err := r.enc.Encode(line(r.seq)); err != nil {
		return err
	}
	return r.out.Flush()
}

// Flush flushes the underlying writer.
func (r *Recorder) Flush() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.out.Flush()
}

// Announce writes the ports sentinel line and flushes it. It goes through
// the recorder's lock so the line is ordered before every frame line, also
// for the race detector (a charger can only dial after reading the line).
func (r *Recorder) Announce(ocppPort, httpPort int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, err := fmt.Fprintf(r.out, "E2E_CSMS_PORTS {\"ocpp\":%d,\"http\":%d}\n", ocppPort, httpPort); err != nil {
		return err
	}
	return r.out.Flush()
}
