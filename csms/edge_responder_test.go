package csms_test

import (
	"context"
	"encoding/json"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type edgeHB struct{}
type edgeHBResp struct {
	CurrentTime string `json:"currentTime"`
}

var edgeHBMsg = ocppj.Message[edgeHB, edgeHBResp]{Action: "Heartbeat", Direction: ocppj.SentByCP}

// edgeMetrics records EdgeMetrics calls.
type edgeMetrics struct {
	mu   sync.Mutex
	seen []string
}

func (m *edgeMetrics) ConnectionCount(string, int)                                 {}
func (m *edgeMetrics) CallStarted(string, string, string)                          {}
func (m *edgeMetrics) CallCompleted(string, string, string, time.Duration, string) {}
func (m *edgeMetrics) PendingCallCount(string, int)                                {}
func (m *edgeMetrics) SchemaValidationFailure(string, string, string)              {}
func (m *edgeMetrics) EdgeResponse(version, action string) {
	m.mu.Lock()
	m.seen = append(m.seen, version+"/"+action)
	m.mu.Unlock()
}
func (m *edgeMetrics) got() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.seen...)
}

// call writes one CALL frame on a raw socket and returns the answer frame.
func rawCall(t *testing.T, c *websocket.Conn, frame string) []json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(frame)))
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	var arr []json.RawMessage
	require.NoError(t, json.Unmarshal(data, &arr))
	return arr
}

func dialRaw(t *testing.T, r *rig, id string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, r.url+id, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	return c
}

// A gateway with the edge Heartbeat responder answers Heartbeat itself: the
// backend never sees it, other actions still go through, the answer is the
// gateway's clock in the encoding the options ask for.
func TestEdgeHeartbeat_AnsweredAtTheGateway(t *testing.T) {
	defer goleak.VerifyNone(t)
	var backendHB, backendBoot atomic.Int64
	em := &edgeMetrics{}
	var onEdge atomic.Value
	enc := schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds}
	r := newRig(t, []csms.Option{csms.WithEdgeHeartbeat(), csms.WithTimeEncoding(enc), csms.WithMetrics(em),
		csms.WithOnEdgeResponse(func(cpID, action string) { onEdge.Store(cpID + "/" + action) })}, nil)
	defer r.close()
	require.NoError(t, csms.On(r.be, edgeHBMsg, func(context.Context, *csms.Conn, edgeHB) (edgeHBResp, error) {
		backendHB.Add(1)
		return edgeHBResp{CurrentTime: "backend"}, nil
	}))
	require.NoError(t, csms.On(r.be, gwBootMsg, func(context.Context, *csms.Conn, gwBoot) (gwBootResp, error) {
		backendBoot.Add(1)
		return gwBootResp{Status: "Accepted", Interval: 60}, nil
	}))
	r.serve()
	c := dialRaw(t, r, "CP_1")
	defer func() { _ = c.CloseNow() }()

	before := time.Now().UTC().Add(-time.Second)
	arr := rawCall(t, c, `[2,"h1","Heartbeat",{}]`)
	require.Equal(t, "3", string(arr[0]))
	require.Equal(t, `"h1"`, string(arr[1]))
	var body edgeHBResp
	require.NoError(t, json.Unmarshal(arr[2], &body))
	ts, err := time.Parse(time.RFC3339, body.CurrentTime)
	require.NoError(t, err)
	require.True(t, ts.After(before) && ts.Before(time.Now().Add(time.Second)), body.CurrentTime)
	require.Regexp(t, `^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ$`, body.CurrentTime, "time encoding of the gateway applies")

	arr = rawCall(t, c, `[2,"b1","BootNotification",{"chargePointVendor":"V","chargePointModel":"M"}]`)
	require.Equal(t, "3", string(arr[0]))

	require.Zero(t, backendHB.Load(), "the backend did not see the Heartbeat")
	require.EqualValues(t, 1, backendBoot.Load())
	require.Equal(t, []string{"1.6/Heartbeat"}, em.got())
	require.Equal(t, "CP_1/Heartbeat", onEdge.Load())
}

// Without the option Heartbeat goes to the backend, and a `when` that says no
// forwards it too.
func TestEdgeHeartbeat_OptInAndPerSession(t *testing.T) {
	defer goleak.VerifyNone(t)
	for name, opts := range map[string][]csms.Option{
		"off":          nil,
		"when_refuses": {csms.WithEdgeHeartbeat(func(r csms.EdgeRequest) bool { return r.Attrs["boot"] == "accepted" })},
	} {
		t.Run(name, func(t *testing.T) {
			var backendHB atomic.Int64
			r := newRig(t, opts, nil)
			defer r.close()
			require.NoError(t, csms.On(r.be, edgeHBMsg, func(context.Context, *csms.Conn, edgeHB) (edgeHBResp, error) {
				backendHB.Add(1)
				return edgeHBResp{CurrentTime: "backend"}, nil
			}))
			r.serve()
			c := dialRaw(t, r, "CP_1")
			defer func() { _ = c.CloseNow() }()
			arr := rawCall(t, c, `[2,"h1","Heartbeat",{}]`)
			require.JSONEq(t, `{"currentTime":"backend"}`, string(arr[2]))
			require.EqualValues(t, 1, backendHB.Load())
		})
	}
}

// A responder for another action; the request is not validated at the edge.
func TestEdgeResponder_CustomAction(t *testing.T) {
	defer goleak.VerifyNone(t)
	r := newRig(t, []csms.Option{csms.WithEdgeResponder("DataTransfer", func(_ context.Context, req csms.EdgeRequest) ([]byte, bool) {
		return []byte(`{"status":"Accepted","echo":` + string(req.Payload) + `}`), true
	})}, nil)
	defer r.close()
	r.serve()
	c := dialRaw(t, r, "CP_1")
	defer func() { _ = c.CloseNow() }()
	arr := rawCall(t, c, `[2,"d1","DataTransfer",{"vendorId":"x"}]`)
	require.JSONEq(t, `{"status":"Accepted","echo":{"vendorId":"x"}}`, string(arr[2]))
}
