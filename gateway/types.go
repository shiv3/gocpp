package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
)

// ProtoVersion is the contract version carried in every Envelope.
const ProtoVersion = 1

// SessionRef identifies one WebSocket session. Epoch is strictly increasing per
// CPID for the sessions of one gateway (and, with a session registry, across
// gateways); a message that carries an older epoch than the session now held is
// stale.
type SessionRef struct {
	CPID      string
	GatewayID string // stable gateway identity, e.g. "gw-3"
	Boot      string // gateway incarnation, random per process start
	Epoch     uint64
}

func (r SessionRef) String() string {
	return fmt.Sprintf("%s@%s/%s#%d", r.CPID, r.GatewayID, r.Boot, r.Epoch)
}

// Kind is the type of an Envelope.
type Kind uint8

const (
	// KindCall is an inbound CALL: the backend must answer with a Reply.
	KindCall Kind = iota + 1
	// KindSend is an inbound OCPP 2.1 SEND: unconfirmed, no Reply.
	KindSend
	// KindSessionOpened announces a new session (see Envelope.Open).
	KindSessionOpened
	// KindSessionClosed announces the end of a session (see Envelope.Close).
	KindSessionClosed
	// KindOrphanResult carries the answer to a CSMS call whose caller is gone.
	KindOrphanResult
)

func (k Kind) String() string {
	switch k {
	case KindCall:
		return "call"
	case KindSend:
		return "send"
	case KindSessionOpened:
		return "session_opened"
	case KindSessionClosed:
		return "session_closed"
	case KindOrphanResult:
		return "orphan_result"
	}
	return fmt.Sprintf("kind(%d)", uint8(k))
}

// Envelope is one message from a gateway to the processing tier.
type Envelope struct {
	Proto      uint32 // contract version, ProtoVersion
	Session    SessionRef
	Version    ocppj.Version // negotiated OCPP version
	Kind       Kind
	DeliveryID string // Boot + per-gateway sequence; unique per dispatch chain
	Attempt    uint32 // 1 for the first dispatch, >1 for a redispatch after a Nack
	MsgID      string // the charger's OCPP message id (Call, Send)
	Action     string
	Payload    []byte // raw JSON exactly as received; never re-encoded
	ReceivedAt time.Time
	// Deadline is when the gateway answers the charger, on the gateway's clock. A
	// receiver on another host must not compare it with its own clock: it uses
	// the budget Deadline.Sub(ReceivedAt), both read from the same clock.
	Deadline time.Time
	Trace    string // W3C traceparent
	Attrs    map[string]string
	// Route says how the gateway routed the message (nil = default pool, no
	// policy); the backend may put it on its handler span.
	Route *Route

	// Open is set for KindSessionOpened.
	Open *SessionInfo
	// Close is set for KindSessionClosed.
	Close *CloseInfo
	// CallID is the CallRequest.CallID of the orphaned call (KindOrphanResult);
	// Action and Payload carry the call's action and the charger's answer, and
	// Error the CALLERROR if the charger answered with one.
	CallID string
	Error  *WireError
}

// SessionInfo is the connection metadata carried by KindSessionOpened.
type SessionInfo struct {
	RemoteAddr    string
	Subprotocol   string
	RequestHeader http.Header
	PeerCerts     [][]byte // DER, leaf first; empty without a client certificate
	Identity      string
	Meta          map[string]string
}

// CloseInfo describes why a session ended (KindSessionClosed).
type CloseInfo struct {
	// Cause is the sentinel name of the cause (see WireError.Sentinel) or "".
	Cause   string
	Message string
}

// Reply is the backend's answer to an inbound CALL.
type Reply struct {
	Payload []byte     // CALLRESULT payload
	Error   *WireError // CALLERROR; Code is the code to put on the wire
	// Nack reports that the handler did not run (backend draining or
	// overloaded): the gateway may redispatch to another backend.
	Nack bool
	// NoReply suppresses the CALLERROR (an ErrorCodeMapper said so).
	NoReply bool
}

// ErrKind says how a WireError is to be rebuilt as a Go error.
type ErrKind string

const (
	// ErrKindCallError is an OCPP CALLERROR (Code, Description, Details).
	ErrKindCallError ErrKind = "call_error"
	// ErrKindSentinel is a well-known error that keeps its identity
	// (errors.Is) across the link; Sentinel is its name.
	ErrKindSentinel ErrKind = "sentinel"
	// ErrKindOther is any other error, carried as text.
	ErrKindOther ErrKind = "other"
)

// WireError is an error as it travels between the tiers. It generalises the
// remote error of multi-instance routing and adds the gateway sentinels.
type WireError struct {
	Kind        ErrKind
	Code        string // CALLERROR code as it is (or was) on the wire
	Description string
	Details     map[string]any
	ResultError bool // a CALLRESULTERROR (OCPP 2.1)
	Sentinel    string
	Message     string
}

// Gateway-specific errors that keep their identity across a link.
var (
	// ErrSessionStale: the call named an epoch that is no longer the session's.
	ErrSessionStale = errors.New("gateway: session epoch is stale")
	// ErrGatewayDraining: the gateway is draining; the call was not sent.
	ErrGatewayDraining = errors.New("gateway: draining")
	// ErrNoBackend: no backend answered within the hold time.
	ErrNoBackend = errors.New("gateway: no backend available")
	// ErrNotDelivered marks a transport error that happened before anything
	// was handed to the peer: the dispatch is safe to retry elsewhere.
	ErrNotDelivered = errors.New("gateway: not delivered")
	// ErrUnsupported: the link or tier does not implement the operation.
	ErrUnsupported = errors.New("gateway: operation not supported")
)

var sentinels = map[string]error{
	"not_connected":      ocppj.ErrNotConnected,
	"conn_closed":        ocppj.ErrConnClosed,
	"conn_closed_server": ocppj.ErrConnClosedByServer,
	"conn_dropped":       ocppj.ErrConnDropped,
	"conn_replaced":      ocppj.ErrConnReplaced,
	"call_timeout":       ocppj.ErrCallTimeout,
	"call_cancelled":     ocppj.ErrCallCancelled,
	"queue_full":         ocppj.ErrQueueFull,
	"concurrent_limit":   ocppj.ErrConcurrentCallLimit,
	"session_stale":      ErrSessionStale,
	"gateway_draining":   ErrGatewayDraining,
	"no_backend":         ErrNoBackend,
	"unsupported":        ErrUnsupported,
	"context_canceled":   context.Canceled,
	"deadline_exceeded":  context.DeadlineExceeded,
}

// SentinelName returns the sentinel name of err ("" if it is none of them).
func SentinelName(err error) string {
	for name, s := range sentinels {
		if errors.Is(err, s) {
			return name
		}
	}
	return ""
}

// SentinelByName returns the sentinel error registered under name, or nil.
func SentinelByName(name string) error { return sentinels[name] }

// EncodeError converts err to its wire form. nil gives nil.
func EncodeError(err error) *WireError {
	if err == nil {
		return nil
	}
	var ce *ocppj.CallError
	if errors.As(err, &ce) {
		return &WireError{Kind: ErrKindCallError, Code: string(ce.Code), Description: ce.Description,
			Details: ce.Details, ResultError: ce.IsResultError}
	}
	if name := SentinelName(err); name != "" {
		return &WireError{Kind: ErrKindSentinel, Sentinel: name, Message: err.Error()}
	}
	return &WireError{Kind: ErrKindOther, Message: err.Error()}
}

// Err rebuilds the Go error. A nil WireError gives nil.
func (e *WireError) Err() error {
	if e == nil {
		return nil
	}
	switch e.Kind {
	case ErrKindCallError:
		return &ocppj.CallError{Code: ocppj.ErrorCode(e.Code), Description: e.Description,
			Details: e.Details, IsResultError: e.ResultError}
	case ErrKindSentinel:
		if s, ok := sentinels[e.Sentinel]; ok {
			return s
		}
	}
	if e.Message == "" {
		return errors.New("gateway: unspecified error")
	}
	return errors.New(e.Message)
}

// Timeouts are per-connection timing parameters (csms.ConnTimeouts without the
// dependency). Zero means "disabled" for PingInterval and ReadTimeout, as for
// the server options.
type Timeouts struct {
	CallTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration
	PongWait     time.Duration
	ReadTimeout  time.Duration
}

// ConnectRequest is what the gateway knows about an incoming upgrade.
type ConnectRequest struct {
	CPID        string // identity extracted from the request path
	Path        string
	RemoteAddr  string
	Header      http.Header
	PeerCerts   [][]byte // DER, leaf first
	Subprotocol []string // offered by the charger
	Gateway     SessionRef
	// Route is the routing decision for the connect ("$connect", no attributes).
	Route *Route
}

// ConnectReply is the backend's decision for an upgrade.
type ConnectReply struct {
	// Reject, if non-nil, refuses the upgrade with that HTTP status.
	Reject *Rejection
	// CPID overrides the charge point id (the authenticated identity).
	CPID string
	// Identity and Meta are stored with the session and handed back in
	// SessionOpened.
	Identity string
	Meta     map[string]string
	// Timeouts, if non-nil, replaces the gateway's connection timing.
	Timeouts *Timeouts
	// Attrs are the initial session attributes.
	Attrs map[string]string
}

// Rejection refuses an upgrade.
type Rejection struct {
	Status  int
	Message string
}

// Upstream is what a gateway needs from the processing tier.
//
// Experimental: this API may change before v1.0.
type Upstream interface {
	// Connect decides an upgrade: authentication, admission, timeouts.
	Connect(ctx context.Context, req ConnectRequest) (ConnectReply, error)
	// Handle runs an inbound CALL and returns the answer to write.
	Handle(ctx context.Context, env Envelope) (Reply, error)
	// Notify delivers an event that needs no answer: SEND, SessionOpened,
	// SessionClosed, OrphanResult. Delivery is at-least-once.
	Notify(ctx context.Context, env Envelope) error
}

// Sent tells a caller whether a CSMS call reached the wire, and so whether a
// retry can duplicate it.
type Sent uint8

const (
	// SentUnknown is the zero value: the gateway did not say.
	SentUnknown Sent = iota
	// NotSent: nothing was written to the charger; retrying is safe.
	NotSent
	// SentNoResponse: the frame was written but no answer arrived (timeout,
	// disconnect); the charger may have executed it.
	SentNoResponse
	// Answered: the charger answered (CALLRESULT or CALLERROR).
	Answered
)

func (s Sent) String() string {
	switch s {
	case NotSent:
		return "not_sent"
	case SentNoResponse:
		return "sent_no_response"
	case Answered:
		return "answered"
	}
	return "unknown"
}

// CallRequest is a CSMS-initiated CALL (or SEND).
type CallRequest struct {
	// Target is the session to call. Epoch 0 means "whatever session the
	// gateway holds for CPID now"; otherwise a different epoch is
	// ErrSessionStale.
	Target SessionRef
	// CallID is a caller-chosen idempotency key: a second request with the same
	// CallID while the first is in flight shares its outcome.
	CallID  string
	Action  string
	Payload []byte // already validated and transformed by the caller
	// Deadline is the caller's absolute deadline, on the caller's clock. Budget is
	// the time left when the request was made; a receiver on another host
	// applies Budget from its own receive time and ignores Deadline when Budget
	// is set, so clock skew between hosts cannot cancel or extend a call.
	Deadline time.Time
	Budget   time.Duration
	Trace    string
	// CallerPool and CallerVersion name the pool and version of the backend that
	// issued the call. A link sets them on the gateway side (they are not part of
	// the wire format); the gateway uses CallerPool to route the OrphanResult of
	// the call back to the pool that asked.
	CallerPool    string
	CallerVersion string
}

// CallReply is the outcome of a CallRequest.
type CallReply struct {
	Payload []byte
	Error   *WireError
	Sent    Sent
	// Session is the session that actually served the call.
	Session SessionRef
}

// ControlOp is an administrative operation on a session.
type ControlOp uint8

const (
	// ControlDisconnect closes the session (normal closure).
	ControlDisconnect ControlOp = iota + 1
	// ControlReplaced closes the session as replaced by a newer one (1008).
	ControlReplaced
	// ControlSetAttrs merges ControlRequest.Attrs into the session attributes
	// (an empty value deletes the key; the total is capped at MaxAttrsSize).
	ControlSetAttrs
	// ControlInvalidateAuth makes the gateway forget its cached connect decisions
	// for Target.CPID (all of them when empty), after a credential change. It
	// needs no session: Target.GatewayID names the gateway, Target.CPID the
	// charge point.
	ControlInvalidateAuth
)

// MaxAttrsSize caps the session attributes (sum of key and value lengths).
const MaxAttrsSize = 1024 // equals storage.MaxAttrBytes

// ErrAttrsTooLarge is returned when a SetAttrs would exceed MaxAttrsSize.
var ErrAttrsTooLarge = errors.New("gateway: session attributes exceed 1 KiB")

// ControlRequest is an administrative request for a session.
type ControlRequest struct {
	Target SessionRef
	Op     ControlOp
	Reason string
	Attrs  map[string]string // ControlSetAttrs
}

// Downstream is what the processing tier (or any other service) needs from
// gateways. A link routes it to the gateway named by Target.GatewayID.
//
// Experimental: this API may change before v1.0.
type Downstream interface {
	Call(ctx context.Context, req CallRequest) (CallReply, error)
	Send(ctx context.Context, req CallRequest) error
	Control(ctx context.Context, req ControlRequest) error
}

// GatewayLink is the gateway's end of a link.
//
// Experimental: this API may change before v1.0.
type GatewayLink interface {
	// Upstream sends work to the attached backends.
	Upstream
	// Attach makes the gateway's Downstream reachable under id. The returned
	// function detaches it.
	Attach(id string, d Downstream) (detach func())
}

// BackendLink is the processing tier's end of a link.
//
// Experimental: this API may change before v1.0.
type BackendLink interface {
	// Downstream reaches the gateway named by each request's Target.
	Downstream
	// Serve attaches up to the link, announcing itself with info, and blocks
	// until ctx ends or the link fails. When ctx ends the backend drains: it takes no new work and Serve
	// returns once work in flight has finished.
	Serve(ctx context.Context, up Upstream, info BackendInfo) error
	// Locate finds the session of a charge point, if the link can.
	Locate(ctx context.Context, cpID string) (Located, bool, error)
}

// Locator is optionally implemented by a gateway's Downstream so a link can
// find the session of a charge point without a registry (the in-process link
// and the gRPC link, which asks every attached gateway, use it).
//
// Experimental: this API may change before v1.0.
type Locator interface {
	LocateSession(cpID string) (Located, bool)
}

// Located is a session found by a Locator.
type Located struct {
	Ref     SessionRef
	Version ocppj.Version
	Info    *SessionInfo
}

// sentError attaches Sent to an error without hiding it from errors.Is/As.
type sentError struct {
	err  error
	sent Sent
}

func (e *sentError) Error() string { return e.err.Error() }
func (e *sentError) Unwrap() error { return e.err }

// WithSent wraps err so that SentOf reports s. A nil err stays nil.
func WithSent(err error, s Sent) error {
	if err == nil {
		return nil
	}
	return &sentError{err: err, sent: s}
}

// SentOf returns what is known about whether the failed CSMS call reached the
// charger (SentUnknown if err carries no such information).
func SentOf(err error) Sent {
	var se *sentError
	if errors.As(err, &se) {
		return se.sent
	}
	return SentUnknown
}

// DefaultPool is the pool of a backend that names none, and the only pool a
// gateway without a routing policy sends work to. Starting a backend in another
// pool is therefore never a traffic change by itself.
const DefaultPool = "default"

// Role of a backend on a link.
type Role uint8

const (
	// RoleWorker takes inbound work (and may call chargers).
	RoleWorker Role = iota
	// RoleCaller only issues Call/Control (admin API, workflow workers).
	RoleCaller
)

// ProtoRange is an inclusive range of link protocol versions.
type ProtoRange struct{ Min, Max uint32 }

// Contains reports whether v is in the range.
func (r ProtoRange) Contains(v uint32) bool { return v >= r.Min && v <= r.Max }

// Capability names (independent of the protocol number). Peers ignore names
// they do not know.
const (
	CapNack       = "nack"
	CapOrphan     = "orphan"
	CapAttrsSet   = "attrs.set"
	CapRouteV1    = "route.v1"
	CapSend21     = "send.21"
	CapCancel     = "cancel"
	CapActionList = "actions.list"
	CapLocate     = "locate"
	CapBatch      = "batch" // several frames per stream message (link-grpc)
)

// BackendInfo is what a backend announces when it attaches to a link.
type BackendInfo struct {
	ID      string            // unique per process; assigned by the link if empty
	Pool    string            // "" = DefaultPool
	Version string            // build version; a metric label, never used for routing
	Role    Role              // RoleWorker | RoleCaller
	Proto   ProtoRange        // link protocol versions it speaks
	Caps    []string          // capability names
	Actions []string          // actions it has handlers for (csms fills it in); empty = all
	Labels  map[string]string // free form, exported in status output only
}

// Has reports whether the backend advertises capability c.
func (b BackendInfo) Has(c string) bool {
	for _, x := range b.Caps {
		if x == c {
			return true
		}
	}
	return false
}

// Handles reports whether the backend serves action: it lists it, or lists
// none (an empty Actions means all).
func (b BackendInfo) Handles(action string) bool {
	if len(b.Actions) == 0 {
		return true
	}
	for _, x := range b.Actions {
		if x == action {
			return true
		}
	}
	return false
}

// Route records a routing decision: where a message went and why.
type Route struct {
	Pool     string
	Fallback string // pool to use when Pool has no available member ("" = none)
	Rule     string // matching rule, "" = default
	Rev      uint64 // policy revision
}

// RouteInput is what the gateway knows about a message without opening the
// payload.
type RouteInput struct {
	CPID   string
	Action string // OCPP action, or "$connect", "$opened", "$closed"
	Kind   Kind
	OCPP   ocppj.Version
	Attrs  map[string]string // session attributes
}

// RouteDecision is a Router's answer. It is the same shape as Route.
type RouteDecision = Route

// Router decides per message which pool serves it. Route must be pure,
// non-blocking and deterministic: the same input and policy give the same
// decision, which makes routing sticky per charger by construction.
//
// Experimental: this API may change before v1.0.
type Router interface {
	Route(in RouteInput) RouteDecision
}

// RouterFunc adapts a function to Router.
type RouterFunc func(in RouteInput) RouteDecision

// Route implements Router.
func (f RouterFunc) Route(in RouteInput) RouteDecision { return f(in) }

// DefaultRouter sends everything to DefaultPool.
type DefaultRouter struct{}

// Route implements Router.
func (DefaultRouter) Route(RouteInput) RouteDecision { return RouteDecision{Pool: DefaultPool} }

// PoolReject is a reserved pool name: a message routed to it is refused at
// once (ErrNoBackend, the charger gets InternalError) instead of being held. A
// router uses it for a suspended rule whose actions only the canary handles.
const PoolReject = "$reject"

// RouteObserver is told what happened to routed messages. A Router that also
// implements it (the routepolicy engine does) is wired to the link with its
// route-observer option; it drives fallback accounting, rule suspension and the
// error-rate guard. Methods are called on the dispatch path and must not block.
//
// Experimental: this API may change before v1.0.
type RouteObserver interface {
	// RouteFallback reports that a message routed by r was sent to r.Fallback
	// because r.Pool had no available member ("unavailable") or every member
	// refused it ("nack").
	RouteFallback(r Route, reason string)
	// RouteOutcome reports how an inbound CALL routed by r ended on r.Pool:
	// "result", "callerror_<Code>", "timeout", "stream_broken" or "no_backend".
	// It is not called for messages that were sent to the fallback pool.
	RouteOutcome(r Route, outcome string)
}

// ExtAttrPrefix is the prefix of the session attributes an AttrSource provides.
const ExtAttrPrefix = "ext."

// AttrSource supplies routing attributes from a system the processing tier does
// not own (a fleet database, a labelling service). The gateway asks it at
// connect, under a short timeout (a failure means no external attributes, and is
// counted), and refreshes the attributes of connected sessions on an interval.
// Keys are prefixed "ext." by the gateway and can never override attributes the
// backend set (ConnectReply.Attrs, SetAttrs). Implementations must be safe for
// concurrent use; see package gateway/attrsource for a file and an HTTP source.
//
// Experimental: this API may change before v1.0.
type AttrSource interface {
	Attrs(ctx context.Context, cpID string) (map[string]string, error)
}
