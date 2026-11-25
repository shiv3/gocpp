//go:build interop

package interop

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
)

// The scenarios drive gocpp through its typed API (csms.On / csms.Call,
// cp.On / cp.Call with the generated message structs) for every action, so the
// generated types, the codec and the schema validation are exercised by what
// the peers really send. INTEROP_API=raw uses the untyped catch-all and
// CallRaw instead (wire framing and schema validation only).

func useTyped() bool { return os.Getenv("INTEROP_API") != "raw" }

// typedMsg binds one generated message to closures over its request and
// response types.
type typedMsg struct {
	action string
	dir    ocppj.Direction
	// handleOnCSMS registers the CSMS-side handler (message sent by the CP).
	handleOnCSMS func(*csms.Server, *responder, *recorder) error
	// handleOnCP registers the CP-side handler (message sent by the CSMS).
	handleOnCP   func(*cp.Client, *responder, *recorder) error
	callFromCSMS func(context.Context, *csms.Conn, []byte) ([]byte, error)
	callFromCP   func(context.Context, *cp.Client, []byte) ([]byte, error)
}

func decodeInto[T any](b []byte) (T, error) {
	var v T
	err := json.Unmarshal(b, &v)
	return v, err
}

// answerTyped builds the typed response from the fixture answer for req.
func answerTyped[Resp any](resp *responder, side, action string, req any, rec *recorder) (Resp, error) {
	var zero Resp
	raw, err := json.Marshal(req)
	if err != nil {
		return zero, err
	}
	out, aerr := resp.answer(side, action, raw)
	if aerr != nil {
		rec.note(action, raw, nil)
		return zero, rawErr(aerr)
	}
	rec.note(action, raw, out)
	r, err := decodeInto[Resp](out)
	if err != nil {
		return zero, fmt.Errorf("fixture answer for %s does not decode into the typed response: %w", action, err)
	}
	return r, nil
}

func mkTyped[Req, Resp any](m ocppj.Message[Req, Resp]) typedMsg {
	t := typedMsg{action: m.Action, dir: m.Direction}
	byCP := m.Direction == ocppj.SentByCP || m.Direction == ocppj.SentByBoth
	byCSMS := m.Direction == ocppj.SentByCSMS || m.Direction == ocppj.SentByBoth
	if byCP {
		t.handleOnCSMS = func(s *csms.Server, resp *responder, rec *recorder) error {
			return csms.On(s, m, func(ctx context.Context, c *csms.Conn, req Req) (Resp, error) {
				return answerTyped[Resp](resp, "csms", m.Action, req, rec)
			})
		}
		t.callFromCP = func(ctx context.Context, c *cp.Client, payload []byte) ([]byte, error) {
			req, err := decodeInto[Req](payload)
			if err != nil {
				return nil, err
			}
			out, err := cp.Call(ctx, c, m, req)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		}
	}
	if byCSMS {
		t.handleOnCP = func(c *cp.Client, resp *responder, rec *recorder) error {
			return cp.On(c, m, func(ctx context.Context, req Req) (Resp, error) {
				return answerTyped[Resp](resp, "station", m.Action, req, rec)
			})
		}
		t.callFromCSMS = func(ctx context.Context, c *csms.Conn, payload []byte) ([]byte, error) {
			req, err := decodeInto[Req](payload)
			if err != nil {
				return nil, err
			}
			out, err := csms.Call(ctx, c, m, req)
			if err != nil {
				return nil, err
			}
			return json.Marshal(out)
		}
	}
	return t
}

// registerTypedCSMS installs typed handlers for every message a CP sends.
func registerTypedCSMS(srv *csms.Server, ver string, resp *responder, rec *recorder) error {
	if !useTyped() {
		return nil
	}
	for _, m := range typedTable[ver] {
		if m.handleOnCSMS != nil {
			if err := m.handleOnCSMS(srv, resp, rec); err != nil {
				return fmt.Errorf("%s: %w", m.action, err)
			}
		}
	}
	return nil
}

// registerTypedCP installs typed handlers for every message a CSMS sends.
func registerTypedCP(c *cp.Client, ver string, resp *responder, rec *recorder) error {
	if !useTyped() {
		return nil
	}
	for _, m := range typedTable[ver] {
		if m.handleOnCP != nil {
			if err := m.handleOnCP(c, resp, rec); err != nil {
				return fmt.Errorf("%s: %w", m.action, err)
			}
		}
	}
	return nil
}

// callFromCSMS sends action to conn through the typed API when there is one.
func callFromCSMS(ctx context.Context, conn *csms.Conn, ver, action string, payload []byte) ([]byte, error) {
	if useTyped() {
		if m, ok := typedTable[ver][action]; ok && m.callFromCSMS != nil {
			return m.callFromCSMS(ctx, conn, payload)
		}
	}
	return csms.CallRaw(ctx, conn, action, payload)
}

func callFromCP(ctx context.Context, c *cp.Client, ver, action string, payload []byte) ([]byte, error) {
	if useTyped() {
		if m, ok := typedTable[ver][action]; ok && m.callFromCP != nil {
			return m.callFromCP(ctx, c, payload)
		}
	}
	return cp.CallRaw(ctx, c, action, payload)
}
