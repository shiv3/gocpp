// Package dispatch sends CSMS-initiated commands to connected charge points
// in gocpp-csms serve mode. Any action the CSMS may send in the charge point's
// negotiated OCPP version is accepted with a raw JSON payload, which is
// validated against the version's request schema before it is sent
// (csms.CallRaw).
//
// Extension point: features that drive charge points (smart charging, setup
// sequences) call Dispatcher.Send rather than talking to csms directly, so
// every command goes through the same validation and timeout.
package dispatch

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/actions"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/httpapi"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
)

// Lookup finds the connection of a charge point.
type Lookup func(cpID string) (*csms.Conn, bool)

// Dispatcher sends commands.
type Dispatcher struct {
	lookup  Lookup
	schemas *schema.Registry
	timeout time.Duration
}

// New returns a Dispatcher. timeout bounds each command (on top of the
// caller's context).
func New(lookup Lookup, schemas *schema.Registry, timeout time.Duration) *Dispatcher {
	return &Dispatcher{lookup: lookup, schemas: schemas, timeout: timeout}
}

// Error is a command refused before anything was sent: Status is the HTTP
// status to answer with (400 invalid, 404 not connected).
type Error struct {
	Status int
	Msg    string
}

func (e *Error) Error() string { return e.Msg }

// Validate checks that action is a CALL the CSMS may send in version v and
// that payload matches its request schema.
func (d *Dispatcher) Validate(v ocppj.Version, action string, payload []byte) error {
	if !actions.SentByCSMS(v).Has(action) {
		return &Error{Status: http.StatusBadRequest, Msg: fmt.Sprintf("unsupported %s action %q", v, action)}
	}
	val, ok := d.schemas.Lookup(string(v), action, "request")
	if !ok {
		return &Error{Status: http.StatusBadRequest, Msg: fmt.Sprintf("no %s request schema for %q", v, action)}
	}
	if err := val.Validate(payload); err != nil {
		return &Error{Status: http.StatusBadRequest, Msg: fmt.Sprintf("payload does not match the %s %s request schema: %v", v, action, err)}
	}
	return nil
}

// Send validates and sends one command to cpID and returns the charge point's
// raw answer. Errors are *Error when nothing was sent; otherwise the call
// failed on the charge point side (an *ocppj.CallError, a timeout, a closed
// connection).
func (d *Dispatcher) Send(ctx context.Context, cpID, action string, payload json.RawMessage) (json.RawMessage, error) {
	conn, ok := d.lookup(cpID)
	if !ok {
		return nil, &Error{Status: http.StatusNotFound, Msg: fmt.Sprintf("charge point %q is not connected", cpID)}
	}
	if len(bytes.TrimSpace(payload)) == 0 {
		payload = json.RawMessage(`{}`)
	}
	if err := d.Validate(conn.Version(), action, payload); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	resp, err := csms.CallRaw(ctx, conn, action, payload)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

// Command is the body of POST /command. The request payload is either the
// "payload" member or, without one, every member other than cpId and
// action (the flat shape of the legacy control API).
type Command struct {
	CPID    string
	Action  string
	Payload json.RawMessage
}

// ParseCommand decodes a POST /command body.
func ParseCommand(body []byte) (Command, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return Command{}, fmt.Errorf("decode command: %w", err)
	}
	var cmd Command
	if raw, ok := fields["cpId"]; ok {
		if err := json.Unmarshal(raw, &cmd.CPID); err != nil {
			return Command{}, fmt.Errorf("decode command: cpId: %w", err)
		}
	}
	if raw, ok := fields["action"]; ok {
		if err := json.Unmarshal(raw, &cmd.Action); err != nil {
			return Command{}, fmt.Errorf("decode command: action: %w", err)
		}
	}
	if cmd.CPID == "" {
		return Command{}, errors.New("cpId is required")
	}
	if cmd.Action == "" {
		return Command{}, errors.New("action is required")
	}
	delete(fields, "cpId")
	delete(fields, "action")
	if raw, ok := fields["payload"]; ok {
		if len(fields) > 1 {
			return Command{}, errors.New(`decode command: "payload" cannot be combined with other request fields`)
		}
		cmd.Payload = raw
		return cmd, nil
	}
	flat, err := json.Marshal(fields)
	if err != nil {
		return Command{}, fmt.Errorf("decode command: %w", err)
	}
	cmd.Payload = flat
	return cmd, nil
}

// Handler serves POST /command: 400 for a malformed or schema-invalid
// command, 404 for a charge point that is not connected, 200 with ok:true and
// the charge point's answer as result, or 200 with ok:false when the charge
// point answered with a CALLERROR (errorCode set) or did not answer in time.
func (d *Dispatcher) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !httpapi.AllowMethods(w, r, http.MethodPost) {
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, fmt.Sprintf("read command: %v", err))
			return
		}
		cmd, err := ParseCommand(body)
		if err != nil {
			httpapi.WriteError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp, err := d.Send(r.Context(), cmd.CPID, cmd.Action, cmd.Payload)
		if err != nil {
			WriteSendError(w, err)
			return
		}
		httpapi.WriteResult(w, resp)
	})
}

// WriteSendError answers an error returned by Send.
func WriteSendError(w http.ResponseWriter, err error) {
	var refused *Error
	if errors.As(err, &refused) {
		httpapi.WriteError(w, refused.Status, refused.Msg)
		return
	}
	resp := httpapi.Response{OK: false, Error: err.Error()}
	var callErr *ocppj.CallError
	if errors.As(err, &callErr) {
		resp.ErrorCode = string(callErr.Code)
	}
	httpapi.WriteJSON(w, http.StatusOK, resp)
}
