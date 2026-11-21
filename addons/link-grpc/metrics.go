package linkgrpc

import "time"

// Metrics receives link events. The labels are bounded: pools and attached
// versions are few and actions are those of the OCPP version. All methods must
// be safe for concurrent use and fast.
type Metrics interface {
	// AttachResult counts one Attach attempt: result is "ok", "proto_mismatch",
	// "hello_timeout" or "error".
	AttachResult(proto uint32, result string)
	// UpstreamRequest records one inbound CALL forwarded to a backend. outcome is
	// "result", "callerror_<Code>", "timeout", "nack", "stream_broken" or "error".
	UpstreamRequest(pool, backendVersion, action, outcome string, d time.Duration)
	// DownstreamCall records one CSMS call served for a backend. outcome is
	// "result", "callerror", "timeout", "not_sent", "no_response" or "error".
	DownstreamCall(callerPool, callerVersion, action, outcome string)
	// Unsupported counts a frame body the peer did not know ("recv") or that the
	// peer reported as unknown ("sent").
	Unsupported(direction, what string)
}

// RoutingMetrics is an optional extension of Metrics (a Metrics that also
// implements it receives these events).
type RoutingMetrics interface {
	// LateReply counts an answer to an inbound CALL that reached the gateway
	// after the gateway had given up on it (deadline or cancel): it is dropped.
	// pool is the pool of the backend that sent it.
	LateReply(pool string)
	// OrphanResult counts the answer to a CSMS call whose caller was gone, as the
	// gateway passes it to the caller's pool. outcome is "delivered",
	// "fallback" (the caller's pool had no backend, the default pool took it) or
	// "dropped" (no backend took it in time).
	OrphanResult(pool, outcome string)
}

// ReportLateReply calls m.LateReply when m implements RoutingMetrics.
func ReportLateReply(m Metrics, pool string) {
	if r, ok := m.(RoutingMetrics); ok {
		r.LateReply(pool)
	}
}

// ReportOrphanResult calls m.OrphanResult when m implements RoutingMetrics.
func ReportOrphanResult(m Metrics, pool, outcome string) {
	if r, ok := m.(RoutingMetrics); ok {
		r.OrphanResult(pool, outcome)
	}
}

// NopMetrics ignores every event.
type NopMetrics struct{}

func (NopMetrics) AttachResult(uint32, string)                        {}
func (NopMetrics) UpstreamRequest(_, _, _, _ string, _ time.Duration) {}
func (NopMetrics) DownstreamCall(_, _, _, _ string)                   {}
func (NopMetrics) Unsupported(_, _ string)                            {}
