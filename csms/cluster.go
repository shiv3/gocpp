package csms

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
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
// does once an instance id or a session registry is configured.
func (s *Server) clustered() bool { return s.sessions != nil }

func (s *Server) globalCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(s.ctx), globalOpTimeout)
}

// initSessions sets up the session manager for a clustered configuration.
func (s *Server) initSessions() {
	cfg := s.cfg
	if cfg.sessionReg == nil && cfg.instanceID == "" {
		return
	}
	scfg := cfg.sessionCfg
	if scfg.GatewayID == "" {
		scfg.GatewayID = cfg.instanceID
	}
	reg, loops := cfg.sessionReg, true
	if reg == nil {
		if sr, ok := cfg.connReg.(storage.SessionRegistry); ok {
			reg = sr
		} else {
			reg, loops = legacyRegistry{reg: cfg.connReg, instanceID: cfg.instanceID}, false
		}
	}
	s.sessions = newSessionManager(reg, scfg, cfg.dispatcher.Logger, loops, s.holders)
}

// holders snapshots the local connections for the session manager.
func (s *Server) holders() []sessionHolder {
	s.mu.RLock()
	defer s.mu.RUnlock()
	hs := make([]sessionHolder, 0, len(s.conns))
	for _, c := range s.conns {
		hs = append(hs, c)
	}
	return hs
}

// claimSession records an upgrade that is about to be accepted. The zero Session
// is returned when the server is not clustered.
func (s *Server) claimSession(r *http.Request, cpID string, attrs map[string]string) (storage.Session, *storage.Session, bool) {
	if s.sessions == nil {
		return storage.Session{}, nil, false
	}
	meta := map[string]string{"remote_addr": r.RemoteAddr}
	for k, v := range attrs {
		if v != "" {
			meta[storage.AttrPrefix+k] = v
		}
	}
	sess, prev := s.sessions.claimPrev(r.Context(), storage.Session{
		CPID: cpID, ConnectedAt: time.Now(), Meta: meta, Version: s.offeredVersion(r),
	})
	return sess, prev, true
}

// SetGatewayLabel sets (or, with an empty value, removes) a label in this
// server's lease and renews the lease, so rollout tooling sees it in
// SessionRegistry.Gateways at once. A no-op without a session registry.
//
// Experimental: this API may change before v1.0.
func (s *Server) SetGatewayLabel(ctx context.Context, key, value string) {
	if s.sessions != nil {
		s.sessions.setLabel(ctx, key, value)
	}
}

// SetSessionAttrs merges attrs into the attributes of the connected charge point id, in
// the local session and (for a clustered server) in the registry, so rollout
// tooling can select it by them. Use it when an attribute becomes known after
// connect (firmware version from BootNotification, say). It returns
// ocppj.ErrNotConnected when the charge point is not connected here. See
// Conn.SetAttrs for the merge rules.
//
// Experimental: this API may change before v1.0.
func (s *Server) SetSessionAttrs(ctx context.Context, id string, attrs map[string]string) error {
	c, ok := s.Get(id)
	if !ok && s.backend != nil {
		h, err := s.lookupGateway(ctx, id)
		if err != nil {
			return err
		}
		return h.SetAttrs(ctx, attrs)
	}
	if !ok {
		return ocppj.ErrNotConnected
	}
	return c.SetAttrs(ctx, attrs)
}

// releaseSession removes the session's record while it is still the current one.
func (s *Server) releaseSession(sess storage.Session) {
	if s.sessions == nil {
		return
	}
	ctx, cancel := s.globalCtx()
	defer cancel()
	s.sessions.release(ctx, sess)
}

// abandonClaim undoes a claim whose upgrade failed. It removes the claimed
// record and then puts back what the claim displaced: the record of a live
// connection of the same id held by this server, else (a session held by
// another gateway) the record the claim overwrote. Without it a failed upgrade
// request for a connected charge point would delete that charge point's route.
func (s *Server) abandonClaim(cpID string, sess storage.Session, prev *storage.Session) {
	if s.sessions == nil {
		return
	}
	ctx, cancel := s.globalCtx()
	defer cancel()
	s.sessions.release(ctx, sess)
	s.mu.RLock()
	cur := s.conns[cpID]
	s.mu.RUnlock()
	var restore *storage.Session
	switch {
	case cur != nil:
		if hs := cur.holderSession(); hs.Epoch != 0 {
			restore = &hs
		}
	case prev != nil && prev.Epoch != 0 && prev.Boot != "" && prev.GatewayID != s.sessions.cfg.GatewayID:
		// Only another gateway's record is put back. A record of this gateway
		// without a live connection here is dead; restoring it would route to
		// a socket that no longer exists.
		restore = prev
	}
	if restore == nil {
		return
	}
	if _, err := s.sessions.reg.Reassert(ctx, *restore); err != nil {
		s.cfg.dispatcher.Logger.Error("csms: restoring session after failed upgrade failed",
			"cp_id", cpID, "epoch", restore.Epoch, "error", err)
		return
	}
	if cur != nil && !cur.alive() {
		// The connection ended meanwhile; its teardown may have run before the
		// record came back.
		s.sessions.release(ctx, *restore)
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
	"gateway_draining":   ocppj.ErrGatewayDraining,
	"drain_timeout":      ocppj.ErrDrainTimeout,
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
// session registry is consulted and, when another instance holds the charge point, a
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
	if s.backend != nil {
		return s.lookupGateway(ctx, id)
	}
	if !s.clustered() {
		return nil, ocppj.ErrNotConnected
	}
	sess, ok, err := s.sessions.reg.Lookup(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("csms: look up %q: %w", id, err)
	}
	if !ok {
		return nil, ocppj.ErrNotConnected
	}
	// Our own entry with no local connection is stale (the disconnect is in
	// flight); there is nothing to forward to.
	if sess.GatewayID == s.sessions.cfg.GatewayID {
		return nil, ocppj.ErrNotConnected
	}
	return &Conn{remoteID: id, srv: s, session: &sess}, nil
}

// offeredVersion is the OCPP version the upgrade will negotiate: the first of
// the server's subprotocols (its preference order, as the WebSocket library
// picks) that the charger offers. Empty when there is none.
func (s *Server) offeredVersion(r *http.Request) string {
	offered := websocketSubprotocols(r)
	for _, ours := range s.cfg.subProtocols {
		for _, p := range offered {
			if strings.EqualFold(ours, p) {
				return strings.TrimPrefix(ours, "ocpp")
			}
		}
	}
	return ""
}
