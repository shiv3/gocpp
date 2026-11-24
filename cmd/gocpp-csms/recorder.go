package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"sync"
)

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

func NewRecorder(out *bufio.Writer) *Recorder {
	return &Recorder{
		enc: json.NewEncoder(out),
		out: out,
	}
}

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
