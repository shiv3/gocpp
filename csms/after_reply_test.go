package csms_test

import (
	"context"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	m16 "github.com/shiv3/gocpp/v16/messages"
	p16 "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// The CALLRESULT of BootNotification must precede the GetConfiguration CALL the
// handler schedules with AfterReply, both on the socket and in the frame
// observer; a handler error runs no callback.
func TestAfterReply_CallResultPrecedesFollowUpCall(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	obs := func(_ context.Context, ev observability.FrameEvent) {
		if ev.Direction != observability.FrameOut {
			return
		}
		mu.Lock()
		defer mu.Unlock()
		switch ev.Type {
		case ocppj.CallResult:
			seen = append(seen, "CALLRESULT")
		case ocppj.Call:
			seen = append(seen, "CALL:"+ev.Action)
		}
	}
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithFrameObserver(obs))
	require.NoError(t, csms.On(srv, p16.BootNotification,
		func(ctx context.Context, c *csms.Conn, req m16.BootNotificationRequest) (m16.BootNotificationResponse, error) {
			if req.ChargePointModel == "bad" {
				require.True(t, csms.AfterReply(ctx, func(context.Context) { t.Error("callback ran after a handler error") }))
				return m16.BootNotificationResponse{}, ocppj.NewCallError(ocppj.ErrorCodeInternalError, "no", nil)
			}
			require.True(t, csms.AfterReply(ctx, func(ctx context.Context) {
				_, _ = csms.Call(ctx, c, p16.GetConfiguration, m16.GetConfigurationRequest{})
			}))
			return m16.BootNotificationResponse{CurrentTime: time.Unix(0, 0).UTC(), Interval: 5, Status: "Accepted"}, nil
		}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP1", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	require.NoError(t, err)
	defer func() { _ = c.Close(websocket.StatusNormalClosure, "") }()

	// Handler error first: the CALLERROR arrives and no follow-up call follows.
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"e1","BootNotification",{"chargePointModel":"bad","chargePointVendor":"V"}]`)))
	_, data, err := c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(data), `[4,"e1"`)

	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"b1","BootNotification",{"chargePointModel":"M","chargePointVendor":"V"}]`)))
	_, data, err = c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(data), `[3,"b1"`, "CALLRESULT first")
	_, data, err = c.Read(ctx)
	require.NoError(t, err)
	require.Contains(t, string(data), `"GetConfiguration"`, "then the scheduled CALL")

	mu.Lock()
	defer mu.Unlock()
	require.Equal(t, []string{"CALLRESULT", "CALL:GetConfiguration"}, seen)
}

func TestAfterReply_OutsideHandler(t *testing.T) {
	require.False(t, csms.AfterReply(context.Background(), func(context.Context) {}))
}
