package csms

import (
	"context"
	"encoding/json"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
)

// EdgeRequest is an inbound CALL a gateway may answer itself.
type EdgeRequest struct {
	CPID    string
	Version ocppj.Version
	Action  string
	Payload []byte            // as received, not validated
	Attrs   map[string]string // session attributes set by the backend (read-only)
}

// EdgeResponder answers an inbound CALL at the gateway, without a backend hop.
// It returns the CALLRESULT payload and true, or false to forward the CALL to the
// processing tier as usual (so a responder can decide per session, for example
// from an attribute the backend set). It runs on the connection's handler
// goroutine and must not block.
type EdgeResponder func(ctx context.Context, req EdgeRequest) (payload []byte, handled bool)

// WithEdgeResponder makes a gateway (NewGatewayServer) answer action itself with
// r. It is opt-in per action and off by default: the gateway becomes
// action-aware for that action, the request is not validated and the processing
// tier never sees the message (use EdgeMetrics counters, or WithOnEdgeResponse,
// if the application tracks liveness). Ignored by other servers.
func WithEdgeResponder(action string, r EdgeResponder) Option {
	return optionFunc(func(c *serverConfig) {
		m := make(map[string]EdgeResponder, len(c.edgeResponders)+1)
		for k, v := range c.edgeResponders {
			m[k] = v
		}
		m[action] = r
		c.edgeResponders = m
	})
}

// WithEdgeHeartbeat answers Heartbeat at the gateway with the gateway's clock
// ({"currentTime": ...}, formatted per WithTimeEncoding, RFC 3339 UTC by
// default). Heartbeats are most of the idle traffic of a fleet; answering them
// here removes it from the backend hop. when, if given, decides per request
// (false forwards to the backend), for example to wait until the backend has
// accepted the charger's BootNotification:
//
//	csms.WithEdgeHeartbeat(func(r csms.EdgeRequest) bool { return r.Attrs["boot"] == "accepted" })
//
// Default off. See WithEdgeResponder for the trade-offs.
func WithEdgeHeartbeat(when ...func(EdgeRequest) bool) Option {
	return optionFunc(func(c *serverConfig) {
		c.edgeHeartbeat = true
		c.edgeHeartbeatWhen = when
	})
}

// edgeResponders is the responder table of a gateway: WithEdgeResponder entries
// plus the Heartbeat responder, which uses the gateway's time encoding.
func (c *serverConfig) buildEdgeResponders() map[string]EdgeResponder {
	m := make(map[string]EdgeResponder, len(c.edgeResponders)+1)
	for k, v := range c.edgeResponders {
		m[k] = v
	}
	if _, set := m["Heartbeat"]; c.edgeHeartbeat && !set {
		when, enc := c.edgeHeartbeatWhen, c.edgeTimeEncoding
		m["Heartbeat"] = func(_ context.Context, req EdgeRequest) ([]byte, bool) {
			for _, w := range when {
				if w != nil && !w(req) {
					return nil, false
				}
			}
			return heartbeatPayload(time.Now(), enc), true
		}
	}
	return m
}

// heartbeatPayload is the Heartbeat CALLRESULT for now.
func heartbeatPayload(now time.Time, enc schema.TimeEncoding) []byte {
	ts := now.UTC().Format(time.RFC3339Nano)
	if out, ok := enc.Format(ts); ok {
		ts = out
	}
	b, _ := json.Marshal(struct {
		CurrentTime string `json:"currentTime"`
	}{ts})
	return b
}

// WithOnEdgeResponse calls fn after the gateway answered a CALL itself, so the
// application can keep its own liveness bookkeeping (it must be fast and must not
// block). Ignored by servers that are not gateways.
func WithOnEdgeResponse(fn func(cpID, action string)) Option {
	return optionFunc(func(c *serverConfig) { c.onEdgeResponse = fn })
}
