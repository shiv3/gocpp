package csms_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

type coerceRig struct {
	ctx context.Context
	ws  *websocket.Conn
	srv *csms.Server
}

func newCoerceRig(t *testing.T, opts ...csms.Option) *coerceRig {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	srv := csms.NewServer(append([]csms.Option{csms.WithSubProtocols("ocpp1.6"), csms.WithSchemaRegistry(reg)}, opts...)...)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/cp1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "") })
	return &coerceRig{ctx: ctx, ws: ws, srv: srv}
}

func (r *coerceRig) call(t *testing.T, frame string) string {
	t.Helper()
	require.NoError(t, r.ws.Write(r.ctx, websocket.MessageText, []byte(frame)))
	_, reply, err := r.ws.Read(r.ctx)
	require.NoError(t, err)
	return string(reply)
}

func TestLenientCoercion_InboundRequests(t *testing.T) {
	jst := time.FixedZone("JST", 9*3600)
	rig := newCoerceRig(t, csms.WithLenientSchema(), csms.WithLenientTimeLocation(jst))

	var start messages.StartTransactionRequest
	var meter messages.MeterValuesRequest
	var status messages.StatusNotificationRequest
	require.NoError(t, csms.On(rig.srv, profiles.StartTransaction, func(_ context.Context, _ *csms.Conn, req messages.StartTransactionRequest) (messages.StartTransactionResponse, error) {
		start = req
		return messages.StartTransactionResponse{IDTagInfo: messages.IDTagInfo{Status: "Accepted"}, TransactionID: 7}, nil
	}))
	require.NoError(t, csms.On(rig.srv, profiles.MeterValues, func(_ context.Context, _ *csms.Conn, req messages.MeterValuesRequest) (messages.MeterValuesResponse, error) {
		meter = req
		return messages.MeterValuesResponse{}, nil
	}))
	require.NoError(t, csms.On(rig.srv, profiles.StatusNotification, func(_ context.Context, _ *csms.Conn, req messages.StatusNotificationRequest) (messages.StatusNotificationResponse, error) {
		status = req
		return messages.StatusNotificationResponse{}, nil
	}))

	require.Contains(t, rig.call(t, `[2,"1","StartTransaction",{"connectorId":1,"idTag":"T","meterStart":0,"timestamp":"2026-01-01 00:00:00"}]`), `[3,"1"`)
	require.True(t, start.Timestamp.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, jst)), start.Timestamp.String())

	require.Contains(t, rig.call(t, `[2,"2","MeterValues",{"connectorId":1,"meterValue":[{"timestamp":"2026-01-01T09:30:00","sampledValue":[{"value":"1"}]}]}]`), `[3,"2"`)
	require.Len(t, meter.MeterValue, 1)
	require.True(t, meter.MeterValue[0].Timestamp.Equal(time.Date(2026, 1, 1, 9, 30, 0, 0, jst)))

	require.Contains(t, rig.call(t, `[2,"3","StatusNotification",{"connectorId":"1","errorCode":"NoError","status":"Available"}]`), `[3,"3"`)
	require.EqualValues(t, 1, status.ConnectorID)
}

func TestLenientCoercion_InboundResponseBool(t *testing.T) {
	rig := newCoerceRig(t, csms.WithLenientSchema())
	conn := waitConn(t, rig.srv)

	type result struct {
		resp messages.GetConfigurationResponse
		err  error
	}
	done := make(chan result, 1)
	go func() {
		resp, err := csms.Call(rig.ctx, conn, profiles.GetConfiguration, messages.GetConfigurationRequest{})
		done <- result{resp, err}
	}()
	_, req, err := rig.ws.Read(rig.ctx)
	require.NoError(t, err)
	id := string(req[4:])
	id = id[:indexByte(id, '"')]
	require.NoError(t, rig.ws.Write(rig.ctx, websocket.MessageText,
		[]byte(`[3,"`+id+`",{"configurationKey":[{"key":"K","readonly":"true"}]}]`)))
	res := <-done
	require.NoError(t, res.err)
	require.Len(t, res.resp.ConfigurationKey, 1)
	require.True(t, res.resp.ConfigurationKey[0].Readonly)
}

func TestStrictSchema_StillRejectsCoercibleValues(t *testing.T) {
	rig := newCoerceRig(t, csms.WithStrictSchema(true))
	require.NoError(t, csms.On(rig.srv, profiles.StatusNotification, func(_ context.Context, _ *csms.Conn, _ messages.StatusNotificationRequest) (messages.StatusNotificationResponse, error) {
		t.Error("handler must not run in strict mode")
		return messages.StatusNotificationResponse{}, nil
	}))
	require.Contains(t, rig.call(t, `[2,"3","StatusNotification",{"connectorId":"1","errorCode":"NoError","status":"Available"}]`), `[4,"3"`)
}

func waitConn(t *testing.T, srv *csms.Server) *csms.Conn {
	t.Helper()
	var c *csms.Conn
	require.Eventually(t, func() bool {
		var ok bool
		c, ok = srv.Get("cp1")
		return ok
	}, 2*time.Second, 10*time.Millisecond)
	return c
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return len(s)
}
