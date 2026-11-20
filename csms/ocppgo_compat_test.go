package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/schema"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/v16"
	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	v201 "github.com/shiv3/gocpp/v201"
	v201messages "github.com/shiv3/gocpp/v201/messages"
	v201profiles "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

func newRigFor(t *testing.T, sub string, reg *schema.Registry, opts ...csms.Option) *coerceRig {
	t.Helper()
	srv := csms.NewServer(append([]csms.Option{csms.WithSubProtocols(sub), csms.WithSchemaRegistry(reg)}, opts...)...)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	ws, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/cp1", &websocket.DialOptions{Subprotocols: []string{sub}})
	require.NoError(t, err)
	t.Cleanup(func() { _ = ws.Close(websocket.StatusNormalClosure, "") })
	return &coerceRig{ctx: ctx, ws: ws, srv: srv}
}

func reg16(t *testing.T) *schema.Registry {
	t.Helper()
	reg := schema.NewRegistry()
	require.NoError(t, v16.RegisterSchemas(reg))
	return reg
}

// The charger-visible answers of ocpp-go, reproduced by WithOCPPGoCompat.
func TestOCPPGoCompat_ErrorCodesAndAcceptance(t *testing.T) {
	rig := newRigFor(t, "ocpp1.6", reg16(t), csms.WithOCPPGoCompat())
	var gotData *string
	require.NoError(t, csms.On(rig.srv, profiles.Heartbeat, func(context.Context, *csms.Conn, messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{CurrentTime: time.Date(2026, 1, 1, 9, 0, 0, 123456789, time.FixedZone("JST", 9*3600))}, nil
	}))
	require.NoError(t, csms.On(rig.srv, profiles.BootNotification, func(context.Context, *csms.Conn, messages.BootNotificationRequest) (messages.BootNotificationResponse, error) {
		return messages.BootNotificationResponse{Status: "Accepted", Interval: 300, CurrentTime: time.Unix(0, 0)}, nil
	}))
	require.NoError(t, csms.On(rig.srv, profiles.DataTransfer, func(_ context.Context, _ *csms.Conn, req messages.DataTransferRequest) (messages.DataTransferResponse, error) {
		gotData = req.Data
		return messages.DataTransferResponse{Status: "Accepted"}, nil
	}))
	require.NoError(t, csms.On(rig.srv, profiles.StatusNotification, func(context.Context, *csms.Conn, messages.StatusNotificationRequest) (messages.StatusNotificationResponse, error) {
		return messages.StatusNotificationResponse{}, nil
	}))

	t.Run("unknown action is NotSupported", func(t *testing.T) {
		require.Equal(t, `[4,"u","NotSupported","action Foo not implemented",{}]`, rig.call(t, `[2,"u","Foo",{}]`))
	})
	t.Run("missing required property keeps the two-r spelling on 1.6", func(t *testing.T) {
		require.Contains(t, rig.call(t, `[2,"m","BootNotification",{"chargePointModel":"m"}]`), `"OccurrenceConstraintViolation"`)
	})
	t.Run("unknown enum is GenericError", func(t *testing.T) {
		require.Contains(t, rig.call(t, `[2,"e","StatusNotification",{"connectorId":1,"errorCode":"NoError","status":"Bogus"}]`), `"GenericError"`)
	})
	t.Run("uncoercible type is FormationViolation", func(t *testing.T) {
		require.Contains(t, rig.call(t, `[2,"t","StatusNotification",{"connectorId":"abc","errorCode":"NoError","status":"Available"}]`), `"FormationViolation"`)
	})
	t.Run("unknown message type is answered on 1.6", func(t *testing.T) {
		require.Contains(t, rig.call(t, `[9,"x","Foo",{}]`), `[4,"x","MessageTypeNotSupported"`)
	})
	t.Run("null payload is accepted and the time goes out as UTC seconds", func(t *testing.T) {
		require.Equal(t, `[3,"n",{"currentTime":"2026-01-01T00:00:00Z"}]`, rig.call(t, `[2,"n","Heartbeat",null]`))
	})
	t.Run("structured DataTransfer data reaches the handler as JSON text", func(t *testing.T) {
		require.Contains(t, rig.call(t, `[2,"d","DataTransfer",{"vendorId":"v","data":{"a":1}}]`), `[3,"d"`)
		require.NotNil(t, gotData)
		require.Equal(t, `{"a":1}`, *gotData)
	})
	t.Run("a long message id is dropped like ocpp-go, the connection survives", func(t *testing.T) {
		require.NoError(t, rig.ws.Write(rig.ctx, websocket.MessageText, []byte(`[2,"`+strings.Repeat("x", 40)+`","Heartbeat",{}]`)))
		require.Contains(t, rig.call(t, `[2,"after","Heartbeat",{}]`), `[3,"after"`)
	})
	t.Run("malformed short CALL is silent", func(t *testing.T) {
		require.NoError(t, rig.ws.Write(rig.ctx, websocket.MessageText, []byte(`[2,"short"]`)))
		require.Contains(t, rig.call(t, `[2,"after2","Heartbeat",{}]`), `[3,"after2"`)
	})
}

func TestOCPPGoCompat_ServerDoesNotPing(t *testing.T) {
	var got csms.ConnTimeouts
	var once sync.Once
	srv := csms.NewServer(csms.WithOCPPGoCompat(), csms.WithConnConfig(func(_ *http.Request, _ string, ct *csms.ConnTimeouts) {
		once.Do(func() { got = *ct })
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/cp1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()
	require.Zero(t, got.PingInterval)
	require.Equal(t, 60*time.Second, got.ReadTimeout)
}

func TestOCPPGoCompat_LaterOptionsWin(t *testing.T) {
	var got csms.ConnTimeouts
	var once sync.Once
	srv := csms.NewServer(csms.WithOCPPGoCompat(), csms.WithWebSocketPingInterval(30*time.Second),
		csms.WithConnConfig(func(_ *http.Request, _ string, ct *csms.ConnTimeouts) { once.Do(func() { got = *ct }) }))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ws, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/cp1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()
	require.Equal(t, 30*time.Second, got.PingInterval)
}

func TestDefaultsUnchangedWithoutCompat(t *testing.T) {
	rig := newRigFor(t, "ocpp1.6", reg16(t), csms.WithStrictSchema(true))
	require.NoError(t, csms.On(rig.srv, profiles.Heartbeat, func(context.Context, *csms.Conn, messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{CurrentTime: time.Date(2026, 1, 1, 9, 0, 0, 500000000, time.FixedZone("JST", 9*3600))}, nil
	}))
	require.Contains(t, rig.call(t, `[2,"u","Foo",{}]`), `"NotImplemented"`)
	require.Equal(t, `[3,"n",{"currentTime":"2026-01-01T09:00:00.5+09:00"}]`, rig.call(t, `[2,"n","Heartbeat",{}]`))
	require.Contains(t, rig.call(t, `[2,"n2","Heartbeat",null]`), `"FormationViolation"`)
	// Strict: an over-long id is answered with a CALLERROR echoing the id.
	long := strings.Repeat("z", 37)
	require.Equal(t, `[4,"`+long+`","FormationViolation","message id longer than 36 characters",{}]`,
		rig.call(t, `[2,"`+long+`","Heartbeat",{}]`))
}

func TestTimeEncoding_OutboundOCPP16(t *testing.T) {
	enc := schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionMillis}
	rig := newRigFor(t, "ocpp1.6", reg16(t), csms.WithTimeEncoding(enc))
	require.NoError(t, csms.On(rig.srv, profiles.Heartbeat, func(context.Context, *csms.Conn, messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{CurrentTime: time.Date(2026, 1, 1, 9, 0, 0, 123456789, time.FixedZone("JST", 9*3600))}, nil
	}))
	// Responses.
	require.Equal(t, `[3,"n",{"currentTime":"2026-01-01T00:00:00.123Z"}]`, rig.call(t, `[2,"n","Heartbeat",{}]`))

	// Requests: ReserveNow.expiryDate.
	conn := waitConn(t, rig.srv)
	go func() {
		_, _ = csms.Call(rig.ctx, conn, profiles.ReserveNow, messages.ReserveNowRequest{
			ConnectorID: 1, IDTag: "T", ReservationID: 1,
			ExpiryDate: time.Date(2026, 1, 1, 9, 0, 0, 0, time.FixedZone("JST", 9*3600)),
		})
	}()
	_, req, err := rig.ws.Read(rig.ctx)
	require.NoError(t, err)
	require.Contains(t, string(req), `"expiryDate":"2026-01-01T00:00:00.000Z"`)
}

func TestTimeEncoding_OutboundOCPP201(t *testing.T) {
	reg := schema.NewRegistry()
	require.NoError(t, v201.RegisterSchemas(reg))
	rig := newRigFor(t, "ocpp2.0.1", reg, csms.WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds}))
	require.NoError(t, csms.On(rig.srv, v201profiles.BootNotification, func(context.Context, *csms.Conn, v201messages.BootNotificationRequest) (v201messages.BootNotificationResponse, error) {
		return v201messages.BootNotificationResponse{Status: "Accepted", Interval: 300,
			CurrentTime: time.Date(2026, 1, 1, 9, 0, 0, 987654321, time.FixedZone("JST", 9*3600))}, nil
	}))
	reply := rig.call(t, `[2,"b","BootNotification",{"reason":"PowerUp","chargingStation":{"model":"m","vendorName":"v"}}]`)
	require.Contains(t, reply, `"currentTime":"2026-01-01T00:00:00Z"`, reply)
}

// The client side: WithTimeEncoding changes what a charge point sends.
func TestTimeEncoding_ClientOutbound(t *testing.T) {
	var mu sync.Mutex
	var frames []string
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"),
		csms.WithFrameObserver(func(_ context.Context, ev observability.FrameEvent) {
			if ev.Direction == observability.FrameIn {
				mu.Lock()
				frames = append(frames, string(ev.Raw))
				mu.Unlock()
			}
		}))
	require.NoError(t, csms.On(srv, profiles.StartTransaction, func(context.Context, *csms.Conn, messages.StartTransactionRequest) (messages.StartTransactionResponse, error) {
		return messages.StartTransactionResponse{IDTagInfo: messages.IDTagInfo{Status: "Accepted"}, TransactionID: 1}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	c := cp.NewClient("cp1", "ws"+ts.URL[len("http"):]+"/ocpp/cp1", cp.WithSubProtocols("ocpp1.6"),
		cp.WithSchemaRegistry(reg16(t)), cp.WithTimeEncoding(schema.TimeEncoding{UTC: true, Precision: schema.TimePrecisionSeconds}))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, c.Connect(ctx))
	_, err := cp.Call(ctx, c, profiles.StartTransaction, messages.StartTransactionRequest{
		ConnectorID: 1, IDTag: "T", Timestamp: time.Date(2026, 1, 1, 9, 0, 0, 5, time.FixedZone("JST", 9*3600)),
	})
	require.NoError(t, err)
	mu.Lock()
	defer mu.Unlock()
	require.Len(t, frames, 1)
	require.Contains(t, frames[0], `"timestamp":"2026-01-01T00:00:00Z"`)
}

func TestMessageIDPolicyOption(t *testing.T) {
	rig := newRigFor(t, "ocpp1.6", reg16(t), csms.WithLenientSchema(), csms.WithMessageIDPolicy(dispatcher.MessageIDReject))
	require.NoError(t, csms.On(rig.srv, profiles.Heartbeat, func(context.Context, *csms.Conn, messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		return messages.HeartbeatResponse{CurrentTime: time.Unix(0, 0)}, nil
	}))
	long := strings.Repeat("q", 37)
	require.Contains(t, rig.call(t, `[2,"`+long+`","Heartbeat",{}]`), `"FormationViolation"`)
}

func TestErrorCodeMapperOption(t *testing.T) {
	rig := newRigFor(t, "ocpp1.6", reg16(t), csms.WithErrorCodeMapper(func(ctx dispatcher.ErrorContext) (string, bool) {
		if ctx.Source == dispatcher.ErrorSourceUnknownAction {
			return "GenericError", true
		}
		return ctx.WireCode, ctx.Reply
	}))
	require.Contains(t, rig.call(t, `[2,"u","Foo",{}]`), `"GenericError"`)
}
