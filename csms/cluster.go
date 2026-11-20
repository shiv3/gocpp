package csms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/storage"
)

// globalOpTimeout bounds registry writes done on connect and disconnect. They
// run detached from the server context so that the route is still removed while
// the server is closing.
const globalOpTimeout = 5 * time.Second

// ErrRemoteConn is returned by operations that need the local socket (such as
// Conn.Close or Send) when they are used on a Conn that stands for a charge point
// connected to another instance; see Server.Lookup.
var ErrRemoteConn = errors.New("csms: operation not supported on a connection held by another instance")

// clustered reports whether the server takes part in multi-instance routing: it
// does once an instance id is configured with WithInstanceID.
func (s *Server) clustered() bool { return s.cfg.instanceID != "" }

func (s *Server) globalCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(s.ctx), globalOpTimeout)
}

// publishGlobal records this instance as the owner of cpID. The last instance to
// register wins.
func (s *Server) publishGlobal(cpID string) {
	if !s.clustered() {
		return
	}
	ctx, cancel := s.globalCtx()
	defer cancel()
	if err := s.cfg.connReg.PutGlobal(ctx, cpID, s.cfg.instanceID); err != nil {
		s.cfg.dispatcher.Logger.Error("csms: register global route failed",
			"cp_id", cpID, "instance", s.cfg.instanceID, "error", err)
	}
}

// retractGlobal removes the global route of cpID, but only while it still names
// this instance: a charge point that reconnected to another instance has already
// replaced the route, and this instance's late disconnect must not erase it.
func (s *Server) retractGlobal(cpID string) {
	if !s.clustered() {
		return
	}
	ctx, cancel := s.globalCtx()
	defer cancel()
	var err error
	if d, ok := s.cfg.connReg.(storage.OwnedGlobalDeleter); ok {
		_, err = d.DeleteGlobalIf(ctx, cpID, s.cfg.instanceID)
	} else {
		// Best effort: not atomic, so a registration landing between the lookup
		// and the delete can still be lost. Registries used across instances
		// should implement storage.OwnedGlobalDeleter.
		var owner string
		var found bool
		owner, found, err = s.cfg.connReg.LookupGlobal(ctx, cpID)
		if err == nil && found && owner == s.cfg.instanceID {
			err = s.cfg.connReg.DeleteGlobal(ctx, cpID)
		}
	}
	if err != nil {
		s.cfg.dispatcher.Logger.Error("csms: remove global route failed",
			"cp_id", cpID, "instance", s.cfg.instanceID, "error", err)
	}
}

// serveRemote runs the router's ServeRemote for the lifetime of the server,
// restarting it after a transport failure until the server context ends.
func (s *Server) serveRemote() {
	for s.ctx.Err() == nil {
		err := s.cfg.router.ServeRemote(s.ctx, s.handleRemote)
		if s.ctx.Err() != nil {
			return
		}
		if err != nil {
			s.cfg.dispatcher.Logger.Error("csms: remote call serving stopped; retrying",
				"instance", s.cfg.instanceID, "error", err)
		}
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// remoteResponse is the payload a serving instance hands back through the
// router. Routers only carry bytes (and flatten errors to text), so the outcome
// of the call, including a typed CallError, travels inside the payload.
type remoteResponse struct {
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *remoteError    `json:"error,omitempty"`
}

type remoteError struct {
	// Kind is "call_error", "sentinel" or "other".
	Kind        string         `json:"kind"`
	Code        string         `json:"code,omitempty"`
	Description string         `json:"description,omitempty"`
	Details     map[string]any `json:"details,omitempty"`
	ResultError bool           `json:"resultError,omitempty"`
	// Sentinel is a key of remoteSentinels; Message is the error text.
	Sentinel string `json:"sentinel,omitempty"`
	Message  string `json:"message,omitempty"`
}

// remoteSentinels are the ocppj errors that keep their identity (errors.Is) when
// a call fails on another instance.
var remoteSentinels = map[string]error{
	"not_connected":      ocppj.ErrNotConnected,
	"conn_closed":        ocppj.ErrConnClosed,
	"conn_closed_server": ocppj.ErrConnClosedByServer,
	"conn_dropped":       ocppj.ErrConnDropped,
	"conn_replaced":      ocppj.ErrConnReplaced,
	"call_timeout":       ocppj.ErrCallTimeout,
	"call_cancelled":     ocppj.ErrCallCancelled,
	"queue_full":         ocppj.ErrQueueFull,
	"concurrent_limit":   ocppj.ErrConcurrentCallLimit,
}

func encodeRemoteError(err error) *remoteError {
	var ce *ocppj.CallError
	if errors.As(err, &ce) {
		return &remoteError{Kind: "call_error", Code: string(ce.Code), Description: ce.Description,
			Details: ce.Details, ResultError: ce.IsResultError}
	}
	for name, sentinel := range remoteSentinels {
		if errors.Is(err, sentinel) {
			return &remoteError{Kind: "sentinel", Sentinel: name, Message: err.Error()}
		}
	}
	return &remoteError{Kind: "other", Message: err.Error()}
}

func decodeRemoteError(re *remoteError) error {
	switch re.Kind {
	case "call_error":
		return &ocppj.CallError{Code: ocppj.ErrorCode(re.Code), Description: re.Description,
			Details: re.Details, IsResultError: re.ResultError}
	case "sentinel":
		if sentinel, ok := remoteSentinels[re.Sentinel]; ok {
			return sentinel
		}
	}
	return errors.New(re.Message)
}

// handleRemote serves a call forwarded by another instance to the charge point
// connected here. The call outcome is always reported in the response payload;
// a non-nil error is returned only when the response cannot be built.
func (s *Server) handleRemote(ctx context.Context, cpID, action string, req []byte) ([]byte, error) {
	var resp remoteResponse
	c, ok := s.Get(cpID)
	if !ok {
		resp.Error = encodeRemoteError(ocppj.ErrNotConnected)
	} else if out, err := dispatcher.DoCall(ctx, c.inner, action, req); err != nil {
		resp.Error = encodeRemoteError(err)
	} else {
		resp.Payload = out
	}
	return json.Marshal(resp)
}

// callRemote forwards a call for a charge point that is not connected to this
// instance and returns the remote outcome unchanged. A charge point nobody holds,
// or a router that cannot forward (single instance), yields ocppj.ErrNotConnected.
func (s *Server) callRemote(ctx context.Context, cpID, action string, payload []byte) ([]byte, error) {
	raw, err := s.cfg.router.CallRemote(ctx, cpID, action, payload)
	if err != nil {
		if errors.Is(err, storage.ErrNotLocal) || errors.Is(err, storage.ErrRouterNotImplemented) {
			return nil, ocppj.ErrNotConnected
		}
		return nil, fmt.Errorf("csms: remote call %s to %q: %w", action, cpID, err)
	}
	var resp remoteResponse
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("csms: decode remote response for %q: %w", cpID, err)
	}
	if resp.Error != nil {
		return nil, decodeRemoteError(resp.Error)
	}
	return []byte(resp.Payload), nil
}

// Lookup returns a handle for charge point id that Call, CallRaw and CallAsync
// accept. A charge point connected to this instance yields its live Conn, exactly
// like Get. Otherwise, in a clustered server (WithInstanceID), the global
// registry is consulted and, when another instance holds the charge point, a
// handle that forwards calls through the message router is returned. The handle
// is a pointer to the charge point's identity, not to a socket: whether the
// owner still holds the connection is only known when a call is made, which then
// fails with ocppj.ErrNotConnected if it does not.
//
// It returns ocppj.ErrNotConnected when no instance holds the charge point.
// Handles for remote charge points support only calls; Close and Send return
// ErrRemoteConn.
func (s *Server) Lookup(ctx context.Context, id string) (*Conn, error) {
	if c, ok := s.Get(id); ok {
		return c, nil
	}
	if !s.clustered() {
		return nil, ocppj.ErrNotConnected
	}
	owner, ok, err := s.cfg.connReg.LookupGlobal(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("csms: look up %q: %w", id, err)
	}
	if !ok {
		return nil, ocppj.ErrNotConnected
	}
	// Our own entry with no local connection is stale (the disconnect is in
	// flight); there is nothing to forward to.
	if owner == s.cfg.instanceID {
		return nil, ocppj.ErrNotConnected
	}
	return &Conn{remoteID: id, srv: s}, nil
}
