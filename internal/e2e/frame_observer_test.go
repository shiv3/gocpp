package e2e

import (
	"context"
	"github.com/shiv3/gocpp/internal/splitmode"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	v16msg "github.com/shiv3/gocpp/v16/messages"
	v16p "github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// TestE2E_FrameObserverAndMessageID checks the public options and that the
// message id seen by the handler, the outbound callback and the observer agree.
func TestE2E_FrameObserverAndMessageID(t *testing.T) {
	var mu sync.Mutex
	var srvEvs, cpEvs []observability.FrameEvent
	collect := func(dst *[]observability.FrameEvent) observability.FrameObserver {
		return func(_ context.Context, ev observability.FrameEvent) {
			mu.Lock()
			*dst = append(*dst, ev)
			mu.Unlock()
		}
	}
	handlerID := make(chan string, 1)
	srv := splitmode.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithFrameObserver(collect(&srvEvs)))
	require.NoError(t, csms.On(srv, v16p.BootNotification, func(ctx context.Context, _ *csms.Conn, _ v16msg.BootNotificationRequest) (v16msg.BootNotificationResponse, error) {
		id, _ := ocppj.MessageIDFromContext(ctx)
		handlerID <- id
		return v16msg.BootNotificationResponse{Status: v16msg.RegistrationStatusAccepted, CurrentTime: time.Now(), Interval: 300}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	client := cp.NewClient("CP_1", "ws"+ts.URL[len("http"):]+"/ocpp/CP_1",
		cp.WithSubProtocols("ocpp1.6"), cp.WithFrameObserver(collect(&cpEvs)))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, client.Connect(ctx))
	defer client.Close()

	var sentID string
	cctx := ocppj.WithMessageIDCallback(ctx, func(id string) { sentID = id })
	_, err := cp.Call(cctx, client, v16p.BootNotification, v16msg.BootNotificationRequest{ChargePointVendor: "A", ChargePointModel: "M"})
	require.NoError(t, err)
	require.NotEmpty(t, sentID)
	require.Equal(t, sentID, <-handlerID)

	mu.Lock()
	defer mu.Unlock()
	find := func(evs []observability.FrameEvent, dir observability.FrameDirection, typ ocppj.MessageType) *observability.FrameEvent {
		for i := range evs {
			if evs[i].Direction == dir && evs[i].Type == typ && evs[i].MsgID == sentID {
				return &evs[i]
			}
		}
		return nil
	}
	out := find(cpEvs, observability.FrameOut, ocppj.Call)
	require.NotNil(t, out)
	require.Equal(t, "BootNotification", out.Action)
	require.NotNil(t, find(srvEvs, observability.FrameIn, ocppj.Call))
	require.NotNil(t, find(srvEvs, observability.FrameOut, ocppj.CallResult))
	require.NotNil(t, find(cpEvs, observability.FrameIn, ocppj.CallResult))
}
