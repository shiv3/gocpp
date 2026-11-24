package csms_test

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	m16 "github.com/shiv3/gocpp/v16/messages"
	p16 "github.com/shiv3/gocpp/v16/profiles"
	m201 "github.com/shiv3/gocpp/v201/messages"
	p201 "github.com/shiv3/gocpp/v201/profiles"
	"github.com/stretchr/testify/require"
)

// callRaw dials the server with the given subprotocol, sends one CALL and
// returns the CALLRESULT payload (or the whole frame when it is not one).
func callRaw(t *testing.T, ts *httptest.Server, sub, cp, action, payload string) []json.RawMessage {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/"+cp, &websocket.DialOptions{Subprotocols: []string{sub}})
	require.NoError(t, err)
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"1","`+action+`",`+payload+`]`)))
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	var frame []json.RawMessage
	require.NoError(t, json.Unmarshal(data, &frame))
	return frame
}

// One Server serves 1.6 and 2.0.1 chargers whose BootNotification payloads
// differ; each reaches the handler registered for its own version.
func TestOnVersion_MultiplexesSameActionAcrossVersions(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.0.1", "ocpp1.6"))
	require.NoError(t, csms.OnVersion(srv, ocppj.V16, p16.BootNotification,
		func(_ context.Context, _ *csms.Conn, req m16.BootNotificationRequest) (m16.BootNotificationResponse, error) {
			require.Equal(t, "Model16", req.ChargePointModel)
			return m16.BootNotificationResponse{CurrentTime: time.Unix(0, 0).UTC(), Interval: 16, Status: "Accepted"}, nil
		}))
	require.NoError(t, csms.OnVersion(srv, ocppj.V201, p201.BootNotification,
		func(_ context.Context, _ *csms.Conn, req m201.BootNotificationRequest) (m201.BootNotificationResponse, error) {
			require.Equal(t, "Model201", req.ChargingStation.Model)
			return m201.BootNotificationResponse{CurrentTime: time.Unix(0, 0).UTC(), Interval: 201, Status: "Accepted"}, nil
		}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	f16 := callRaw(t, ts, "ocpp1.6", "CP16", "BootNotification", `{"chargePointModel":"Model16","chargePointVendor":"V"}`)
	require.JSONEq(t, "3", string(f16[0]))
	require.Contains(t, string(f16[2]), `"interval":16`)

	f201 := callRaw(t, ts, "ocpp2.0.1", "CP201", "BootNotification",
		`{"reason":"PowerUp","chargingStation":{"model":"Model201","vendorName":"V"}}`)
	require.JSONEq(t, "3", string(f201[0]))
	require.Contains(t, string(f201[2]), `"interval":201`)
}

// A version-agnostic On handler stays the default for versions without their
// own, and a version-scoped handler beats it; OnAny only serves what is left.
func TestOnVersion_FallbackOrder(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp2.0.1", "ocpp1.6"))
	which := make(chan string, 4)
	require.NoError(t, csms.On(srv, p16.Heartbeat,
		func(context.Context, *csms.Conn, m16.HeartbeatRequest) (m16.HeartbeatResponse, error) {
			which <- "any-version"
			return m16.HeartbeatResponse{CurrentTime: time.Unix(0, 0).UTC()}, nil
		}))
	require.NoError(t, csms.OnVersion(srv, ocppj.V201, p201.Heartbeat,
		func(context.Context, *csms.Conn, m201.HeartbeatRequest) (m201.HeartbeatResponse, error) {
			which <- "v201"
			return m201.HeartbeatResponse{CurrentTime: time.Unix(0, 0).UTC()}, nil
		}))
	csms.OnAny(srv, func(_ context.Context, _ *csms.Conn, action string, _ []byte) ([]byte, error) {
		which <- "onany:" + action
		return []byte(`{}`), nil
	})
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	callRaw(t, ts, "ocpp1.6", "A", "Heartbeat", `{}`)
	require.Equal(t, "any-version", <-which)
	callRaw(t, ts, "ocpp2.0.1", "B", "Heartbeat", `{}`)
	require.Equal(t, "v201", <-which)
	callRaw(t, ts, "ocpp2.0.1", "C", "Other", `{}`)
	require.Equal(t, "onany:Other", <-which)
}
