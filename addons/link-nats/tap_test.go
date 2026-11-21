package linknats_test

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/nats-io/nats.go"
	linknats "github.com/shiv3/gocpp/addons/link-nats"
	"github.com/shiv3/gocpp/core/observability"
	"github.com/shiv3/gocpp/core/observability/tap"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

func TestTapSink_PublishesFramesAndSessionsPerChargePoint(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	ns := startNATS(t)
	sub := connect(t, ns.ClientURL())
	pub := connect(t, ns.ClientURL())

	var mu sync.Mutex
	got := map[string]tap.Event{}
	_, err := sub.Subscribe("tapx.tap.>", func(m *nats.Msg) {
		var ev tap.Event
		if json.Unmarshal(m.Data, &ev) == nil {
			mu.Lock()
			got[m.Subject] = ev
			mu.Unlock()
		}
	})
	require.NoError(t, err)
	require.NoError(t, sub.Flush())

	ex := tap.New(linknats.NewTapSink(pub, linknats.WithTapPrefix("tapx")), tap.WithSource("gw"), tap.WithBatch(1, time.Hour))
	ex.FrameObserver()(context.Background(), observability.FrameEvent{
		CPID: "CP.1", Version: ocppj.V16, Direction: observability.FrameIn, Raw: []byte(`[2,"m","Heartbeat",{}]`),
		Type: ocppj.Call, MsgID: "m", Action: "Heartbeat", Time: time.Now(),
	})
	ex.SessionObserver()(context.Background(), observability.SessionEvent{Kind: observability.SessionOpened, CPID: "CP.1", Epoch: 3})
	require.NoError(t, ex.Close(context.Background()))

	require.Eventually(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(got) == 2 }, 3*time.Second, 10*time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	f := got["tapx.tap.frame.CP_1"]
	require.Equal(t, "Heartbeat", f.Action)
	require.Equal(t, "gw", f.Source)
	s := got["tapx.tap.session.CP_1"]
	require.Equal(t, "opened", s.Session)
	require.EqualValues(t, 3, s.Epoch)
}
