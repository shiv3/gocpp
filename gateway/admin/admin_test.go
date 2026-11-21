package admin_test

import (
	"context"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/cp"
	"github.com/shiv3/gocpp/csms"
	"github.com/shiv3/gocpp/gateway"
	"github.com/shiv3/gocpp/gateway/admin"
	"github.com/shiv3/gocpp/gateway/inproc"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

type hbReq struct{}
type hbResp struct {
	CurrentTime string `json:"currentTime"`
}

var hbMsg = ocppj.Message[hbReq, hbResp]{Action: "Heartbeat", Direction: ocppj.SentByCP}

type fixture struct {
	gw      *csms.Server
	wsURL   string
	adminTS *httptest.Server
	cl      admin.Client
	dropped atomic.Int64
	clients []*cp.Client
}

func newFixture(t *testing.T, token string) *fixture {
	t.Helper()
	f := &fixture{}
	hub := inproc.New()
	f.gw = csms.NewGatewayServer(hub.Gateway(), csms.WithGatewayID("gw-test"))
	be := csms.NewServer(csms.WithGatewayLink(hub.Backend()))
	require.NoError(t, csms.On(be, hbMsg, func(context.Context, *csms.Conn, hbReq) (hbResp, error) {
		return hbResp{CurrentTime: time.Now().UTC().Format(time.RFC3339)}, nil
	}))
	stop := hub.AttachBackend(be.Upstream(), gateway.BackendInfo{})
	ws := httptest.NewServer(f.gw.Handler())
	f.wsURL = "ws" + ws.URL[4:]
	dctx, dcancel := context.WithCancel(context.Background())
	f.adminTS = httptest.NewServer(admin.Handler(f.gw, admin.Options{ID: "gw-test", Version: "v1", Token: token, Context: dctx}))
	f.cl = admin.Client{Token: token}
	t.Cleanup(func() {
		dcancel()
		for _, c := range f.clients {
			c.Close()
		}
		f.adminTS.Close()
		ws.Close()
		f.gw.Close()
		_ = stop(context.Background())
		be.Close()
	})
	return f
}

func (f *fixture) connect(t *testing.T, n int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for i := 0; i < n; i++ {
		id := "CP_" + strconv.Itoa(len(f.clients))
		c := cp.NewClient(id, f.wsURL+"/ocpp/"+id, cp.WithSubProtocols("ocpp1.6"),
			cp.WithOnDisconnect(func(error) { f.dropped.Add(1) }))
		require.NoError(t, c.Connect(ctx))
		_, err := cp.Call(ctx, c, hbMsg, hbReq{})
		require.NoError(t, err)
		f.clients = append(f.clients, c)
	}
}

func TestShedStatusAndSoftMax(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	f := newFixture(t, "")
	f.connect(t, 6)
	ctx := context.Background()
	st, err := f.cl.Status(ctx, f.adminTS.URL)
	require.NoError(t, err)
	require.Equal(t, admin.Status{ID: "gw-test", Version: "v1", Sessions: 6}, withoutDrain(st))

	// The default minimum age keeps young sessions where they are.
	_, err = f.cl.Shed(ctx, f.adminTS.URL, admin.ShedRequest{Count: 3, Rate: 100})
	require.NoError(t, err)
	st, err = f.cl.WaitShed(ctx, f.adminTS.URL, 20*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, 6, st.Sessions)
	require.Zero(t, f.dropped.Load())

	// With the age check off, exactly Count sessions are closed, the server keeps serving.
	_, err = f.cl.Shed(ctx, f.adminTS.URL, admin.ShedRequest{Count: 3, MinAge: admin.Duration(-time.Second), Rate: 100})
	require.NoError(t, err)
	st, err = f.cl.WaitShed(ctx, f.adminTS.URL, 20*time.Millisecond)
	require.NoError(t, err)
	require.Eventually(t, func() bool { return f.dropped.Load() == 3 }, 3*time.Second, 20*time.Millisecond)
	require.Eventually(t, func() bool { s, _ := f.cl.Status(ctx, f.adminTS.URL); return s.Sessions == 3 }, 3*time.Second, 20*time.Millisecond)
	require.False(t, st.Draining, "a partial drain is not a full drain")

	// Soft cap.
	st, err = f.cl.SetSoftMax(ctx, f.adminTS.URL, 3)
	require.NoError(t, err)
	require.Equal(t, 3, st.SoftMax)
	require.Equal(t, 3, f.gw.SoftMaxConnections())
	_, err = f.cl.SetSoftMax(ctx, f.adminTS.URL, 0)
	require.NoError(t, err)
}

func withoutDrain(s admin.Status) admin.Status { s.Drain = csms.DrainStatus{}; return s }

func TestShedConflictAndBadRequests(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	f := newFixture(t, "")
	f.connect(t, 4)
	ctx := context.Background()
	_, err := f.cl.Shed(ctx, f.adminTS.URL, admin.ShedRequest{Count: 2, MinAge: admin.Duration(-time.Second), Rate: 0.5})
	require.NoError(t, err)
	_, err = f.cl.Shed(ctx, f.adminTS.URL, admin.ShedRequest{Count: 1, MinAge: admin.Duration(-time.Second)})
	require.ErrorContains(t, err, "409")
	st, err := f.cl.Status(ctx, f.adminTS.URL)
	require.NoError(t, err)
	require.True(t, st.PartialDrain)
	_, err = f.cl.Shed(ctx, f.adminTS.URL, admin.ShedRequest{Count: 0})
	require.ErrorContains(t, err, "400")
	// let it finish quickly: nothing to wait for, the test ends and Close stops it
}

func TestAuthToken(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	f := newFixture(t, "s3cret")
	ctx := context.Background()
	_, err := admin.Client{}.Status(ctx, f.adminTS.URL)
	require.ErrorContains(t, err, "401")
	_, err = admin.Client{Token: "wrong"}.Status(ctx, f.adminTS.URL)
	require.ErrorContains(t, err, "401")
	_, err = f.cl.Status(ctx, f.adminTS.URL)
	require.NoError(t, err)
}

func TestPlanRebalance(t *testing.T) {
	gws := []admin.Load{
		{ID: "gw-0", Sessions: 1000, Serving: true}, {ID: "gw-1", Sessions: 1000, Serving: true},
		{ID: "gw-2", Sessions: 0, Serving: true}, {ID: "gw-old", Sessions: 500, Serving: false},
	}
	require.InDelta(t, 666.7, admin.FairShare(gws), 0.1)
	plan := admin.PlanRebalance(gws, 1.1)
	require.Equal(t, []admin.Shed{{ID: "gw-0", Count: 333}, {ID: "gw-1", Count: 333}}, plan)
	require.False(t, admin.Balanced(gws, 1.1))

	even := []admin.Load{{ID: "a", Sessions: 105, Serving: true}, {ID: "b", Sessions: 100, Serving: true}, {ID: "c", Sessions: 95, Serving: true}}
	require.Empty(t, admin.PlanRebalance(even, 1.1))
	require.True(t, admin.Balanced(even, 1.1))
	// 46/45/29: nobody is far above fair (40) but one is far below: still a plan.
	skew := []admin.Load{{ID: "a", Sessions: 46, Serving: true}, {ID: "b", Sessions: 45, Serving: true}, {ID: "c", Sessions: 29, Serving: true}}
	require.False(t, admin.Balanced(skew, 1.15))
	require.Equal(t, []admin.Shed{{ID: "a", Count: 6}, {ID: "b", Count: 5}}, admin.PlanRebalance(skew, 1.15))
	require.Empty(t, admin.PlanRebalance(nil, 1.1))
	require.Empty(t, admin.PlanRebalance([]admin.Load{{ID: "x", Sessions: 5, Serving: false}}, 1.1))
}
