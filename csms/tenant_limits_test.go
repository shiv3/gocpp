package csms_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
	"go.uber.org/goleak"
)

// tenantMetrics records TenantMetrics calls.
type tenantMetrics struct {
	edgeMetrics
	mu       sync.Mutex
	sessions map[string]int
	rejected map[string]int
	infl     map[string]int
}

func newTenantMetrics() *tenantMetrics {
	return &tenantMetrics{sessions: map[string]int{}, rejected: map[string]int{}, infl: map[string]int{}}
}
func (m *tenantMetrics) TenantSessions(t string, d int) {
	m.mu.Lock()
	m.sessions[t] += d
	m.mu.Unlock()
}
func (m *tenantMetrics) TenantRejected(t, kind string) {
	m.mu.Lock()
	m.rejected[t+"/"+kind]++
	m.mu.Unlock()
}
func (m *tenantMetrics) TenantInflight(t string, d int) {
	m.mu.Lock()
	m.infl[t] += d
	m.mu.Unlock()
}
func (m *tenantMetrics) inflight(t string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.infl[t]
}
func (m *tenantMetrics) rejections() map[string]int {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := map[string]int{}
	for k, v := range m.rejected {
		r[k] = v
	}
	return r
}

// tenantServer is an embedded server whose tenant is the X-Tenant header.
func tenantServer(t *testing.T, l csms.TenantLimits, m *tenantMetrics) string {
	t.Helper()
	l.Resolver = csms.TenantFromRequest(func(r *http.Request) (string, bool) {
		v := r.Header.Get("X-Tenant")
		return v, v != ""
	})
	opts := []csms.Option{csms.WithTenantLimits(l)}
	if m != nil {
		opts = append(opts, csms.WithMetrics(m))
	}
	srv := csms.NewServer(opts...)
	require.NoError(t, csms.On(srv, edgeHBMsg, func(context.Context, *csms.Conn, edgeHB) (edgeHBResp, error) {
		return edgeHBResp{CurrentTime: "now"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { ts.Close(); srv.Close() })
	return "ws" + ts.URL[len("http"):] + "/ocpp/"
}

func dialTenant(url, id, tenant string) (*websocket.Conn, *http.Response, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if tenant != "" {
		h.Set("X-Tenant", tenant)
	}
	return websocket.Dial(ctx, url+id, &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, HTTPHeader: h})
}

func TestTenantLimits_SessionCapRefusesBeforeUpgrade(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	m := newTenantMetrics()
	url := tenantServer(t, csms.TenantLimits{
		Tenants: map[string]csms.TenantLimit{"a": {MaxSessions: 2}}, RetryAfter: 10 * time.Second,
	}, m)

	var conns []*websocket.Conn
	defer func() {
		for _, c := range conns {
			_ = c.CloseNow()
		}
	}()
	for _, id := range []string{"A1", "A2"} {
		c, _, err := dialTenant(url, id, "a")
		require.NoError(t, err)
		conns = append(conns, c)
	}
	// The third is refused with a plain 503 and a jittered Retry-After, before any
	// WebSocket exists.
	_, resp, err := dialTenant(url, "A3", "a")
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	ra, convErr := strconv.Atoi(resp.Header.Get("Retry-After"))
	require.NoError(t, convErr)
	require.InDelta(t, 10, ra, 6)

	// Another tenant (no limit) and a charger without a tenant are unaffected.
	for i, tn := range []string{"b", ""} {
		c, _, err := dialTenant(url, "X"+strconv.Itoa(i), tn)
		require.NoError(t, err)
		conns = append(conns, c)
	}

	// A charger replacing its own live socket is let through at the cap.
	c, _, err := dialTenant(url, "A1", "a")
	require.NoError(t, err)
	conns = append(conns, c)

	// Freeing a slot admits the next one. (Closing the replaced and the replacing
	// sockets both release their slots.)
	require.NoError(t, conns[0].CloseNow()) // the displaced socket (a raw client never answers its close)
	require.NoError(t, conns[1].CloseNow())
	require.Eventually(t, func() bool {
		c, _, err := dialTenant(url, "A3", "a")
		if err == nil {
			conns = append(conns, c)
		}
		return err == nil
	}, 3*time.Second, 20*time.Millisecond)

	rej := m.rejections()
	require.GreaterOrEqual(t, rej["a/sessions"], 1)
}

func TestTenantLimits_TenantFromSessionAttributes(t *testing.T) {
	defer goleak.VerifyNone(t)
	srv := csms.NewServer(
		csms.WithSessionAttributes(func(r *http.Request, cpID string) map[string]string {
			return map[string]string{"tenant": cpID[:2]} // a cp id mapping: the first two characters
		}),
		csms.WithTenantLimits(csms.TenantLimits{Default: csms.TenantLimit{MaxSessions: 1}}),
	)
	ts := httptest.NewServer(srv.Handler())
	defer func() { ts.Close(); srv.Close() }()
	url := "ws" + ts.URL[len("http"):] + "/ocpp/"
	c1, _, err := dialTenant(url, "t1-a", "")
	require.NoError(t, err)
	defer func() { _ = c1.CloseNow() }()
	_, resp, err := dialTenant(url, "t1-b", "")
	require.Error(t, err)
	require.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	c2, _, err := dialTenant(url, "t2-a", "")
	require.NoError(t, err)
	_ = c2.CloseNow()
}

func hb(t *testing.T, c *websocket.Conn, n int) string {
	t.Helper()
	arr := rawCall(t, c, `[2,"h`+strconv.Itoa(n)+`","Heartbeat",{}]`)
	if string(arr[0]) == "3" {
		return "ok"
	}
	var code string
	require.NoError(t, json.Unmarshal(arr[2], &code))
	return code
}

// One noisy charger of a tenant cannot starve a quiet one, and a tenant over its
// rate does not touch another tenant: the over-budget frames are answered with a
// CALLERROR and the sockets stay up.
func TestTenantLimits_MessageRateIsFairWithinAndAcrossTenants(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	m := newTenantMetrics()
	url := tenantServer(t, csms.TenantLimits{
		Tenants: map[string]csms.TenantLimit{"a": {MessageRate: 20, MessageBurst: 20}},
	}, m)
	dial := func(id, tn string) *websocket.Conn {
		c, _, err := dialTenant(url, id, tn)
		require.NoError(t, err)
		return c
	}
	noisy, quiet, other := dial("N", "a"), dial("Q", "a"), dial("O", "b")
	defer func() { _ = noisy.CloseNow(); _ = quiet.CloseNow(); _ = other.CloseNow() }()

	var rejected, accepted int
	quietFailures, quietSent := 0, 0
	nextQuiet := time.Now()
	deadline := time.Now().Add(1500 * time.Millisecond)
	for i := 0; time.Now().Before(deadline); i++ {
		if hb(t, noisy, i) == "ok" {
			accepted++
		} else {
			rejected++
		}
		if !time.Now().Before(nextQuiet) { // the quiet charger: 10 messages per second
			nextQuiet = nextQuiet.Add(100 * time.Millisecond)
			quietSent++
			if hb(t, quiet, i) != "ok" {
				quietFailures++
			}
			require.Equal(t, "ok", hb(t, other, i), "another tenant has no limit")
		}
	}
	require.Greater(t, rejected, 0, "the noisy charger was throttled")
	require.Greater(t, quietSent, 5)
	require.Zero(t, quietFailures, "the quiet charger of the same tenant was not")
	// The tenant's budget is 20/s with a burst of 20: over 1.5 s at most about 50
	// messages from it, of which the noisy one got all but a few.
	require.LessOrEqual(t, accepted, 20+int(1.6*20)+5)

	rej := m.rejections()
	require.Equal(t, rejected, rej["a/messages"], "metrics count every refusal")
	require.Zero(t, rej["b/messages"])
}

// A refused CALL is answered GenericError by default, and RejectCode changes it.
func TestTenantLimits_RejectCode(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	url := tenantServer(t, csms.TenantLimits{
		Default:    csms.TenantLimit{MessageRate: 1, MessageBurst: 1},
		RejectCode: ocppj.ErrorCodeInternalError,
	}, nil)
	c, _, err := dialTenant(url, "C", "z")
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()
	require.Equal(t, "ok", hb(t, c, 1))
	require.Equal(t, "InternalError", hb(t, c, 2))
}

// inflightRig is an embedded server whose Heartbeat handler parks until released,
// so calls stay in flight for as long as a test wants.
type inflightRig struct {
	url     string
	release chan struct{}
	started chan struct{}
	m       *tenantMetrics
	once    sync.Once
}

func (r *inflightRig) unpark() { r.once.Do(func() { close(r.release) }) }

func newInflightRig(t *testing.T, l csms.TenantLimits) *inflightRig {
	t.Helper()
	r := &inflightRig{release: make(chan struct{}), started: make(chan struct{}, 256), m: newTenantMetrics()}
	l.Resolver = csms.TenantFromRequest(func(req *http.Request) (string, bool) {
		v := req.Header.Get("X-Tenant")
		return v, v != ""
	})
	srv := csms.NewServer(csms.WithTenantLimits(l), csms.WithMetrics(r.m))
	require.NoError(t, csms.On(srv, edgeHBMsg, func(ctx context.Context, _ *csms.Conn, _ edgeHB) (edgeHBResp, error) {
		r.started <- struct{}{}
		select {
		case <-r.release:
		case <-ctx.Done():
		}
		return edgeHBResp{CurrentTime: "now"}, nil
	}))
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(func() { r.unpark(); ts.Close(); srv.Close() })
	r.url = "ws" + ts.URL[len("http"):] + "/ocpp/"
	return r
}

// park sends a Heartbeat on a fresh charger of tenant. The returned channel
// receives the reply: "ok" or the CALLERROR code. A refused call answers at once,
// an admitted one only after the rig releases its handler.
func (r *inflightRig) park(t *testing.T, id, tenant string) <-chan string {
	t.Helper()
	c, _, err := dialTenant(r.url, id, tenant)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan string, 1)
	var wg sync.WaitGroup
	t.Cleanup(func() { cancel(); _ = c.CloseNow(); wg.Wait() })
	require.NoError(t, c.Write(ctx, websocket.MessageText, []byte(`[2,"p1","Heartbeat",{}]`)))
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, data, err := c.Read(ctx)
		if err != nil {
			return
		}
		var arr []json.RawMessage
		if json.Unmarshal(data, &arr) != nil {
			return
		}
		if string(arr[0]) == "3" {
			out <- "ok"
			return
		}
		var code string
		_ = json.Unmarshal(arr[2], &code)
		out <- code
	}()
	return out
}

// settle waits for the refusals to arrive and counts the replies received so far.
func settle(chans []<-chan string) (replies []string) {
	time.Sleep(300 * time.Millisecond)
	for _, ch := range chans {
		select {
		case v := <-ch:
			replies = append(replies, v)
		default:
		}
	}
	return replies
}

func repeatStr(s string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = s
	}
	return out
}

// A noisy tenant with many chargers cannot take the capacity a quiet tenant is
// entitled to: the quiet tenant is admitted up to its share however busy the
// noisy one is, and the noisy one is refused, with its sessions kept.
func TestTenantLimits_InFlightPoolNoisyVersusQuiet(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	r := newInflightRig(t, csms.TenantLimits{InFlightPool: 4})

	// The quiet tenant has one idle session: its share (2 of 4) stays reserved.
	quietIdle, _, err := dialTenant(r.url, "Q0", "quiet")
	require.NoError(t, err)
	defer func() { _ = quietIdle.CloseNow() }()

	var noisy []<-chan string
	for i := 0; i < 8; i++ {
		noisy = append(noisy, r.park(t, "N"+strconv.Itoa(i), "noisy"))
	}
	// Only the noisy tenant's own share runs; it cannot borrow the quiet one's.
	require.Equal(t, repeatStr("GenericError", 6), settle(noisy))
	require.Len(t, r.started, 2)

	// The quiet tenant gets its share right away while the noisy one is saturated.
	q := []<-chan string{r.park(t, "Q1", "quiet"), r.park(t, "Q2", "quiet")}
	require.Empty(t, settle(q), "both quiet calls were admitted, none refused")
	require.Len(t, r.started, 4)
	rej := r.m.rejections()
	require.Equal(t, 6, rej["noisy/inflight"])
	require.Zero(t, rej["quiet/inflight"])

	// A noisy session that was refused is still up and is admitted again once
	// capacity frees.
	r.unpark()
	require.Eventually(t, func() bool { return len(r.started) == 4 && r.m.inflight("noisy") == 0 && r.m.inflight("quiet") == 0 },
		3*time.Second, 20*time.Millisecond)
	again := r.park(t, "N9", "noisy")
	require.Equal(t, []string{"ok"}, settle([]<-chan string{again}))
}

// With the other tenants idle (no sessions) a tenant may use the whole pool:
// capacity is not wasted, and the slots come back when the handlers return.
func TestTenantLimits_InFlightPoolIsWorkConserving(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	r := newInflightRig(t, csms.TenantLimits{InFlightPool: 4})
	var chans []<-chan string
	for i := 0; i < 6; i++ {
		chans = append(chans, r.park(t, "S"+strconv.Itoa(i), "solo"))
	}
	require.Equal(t, repeatStr("GenericError", 2), settle(chans))
	require.Len(t, r.started, 4)
	r.unpark()
	require.Eventually(t, func() bool { return r.m.inflight("solo") == 0 }, 3*time.Second, 20*time.Millisecond)
	require.Equal(t, 0, r.m.rejections()["solo/messages"])
}

// Weights split the pool: with weights 3:1 over a pool of 8 and both tenants
// busy, the heavy tenant is entitled to 6 slots and the light one to 2.
func TestTenantLimits_InFlightPoolWeights(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	r := newInflightRig(t, csms.TenantLimits{
		InFlightPool: 8,
		Tenants: map[string]csms.TenantLimit{
			"heavy": {Weight: 3},
			"light": {Weight: 1},
		},
	})
	idleHeavy, _, err := dialTenant(r.url, "H0", "heavy")
	require.NoError(t, err)
	defer func() { _ = idleHeavy.CloseNow() }()
	var light []<-chan string
	for i := 0; i < 8; i++ {
		light = append(light, r.park(t, "L"+strconv.Itoa(i), "light"))
	}
	// The light tenant gets 2; the heavy tenant's 6 stay reserved.
	require.Equal(t, repeatStr("GenericError", 6), settle(light))
	var heavy []<-chan string
	for i := 1; i <= 6; i++ {
		heavy = append(heavy, r.park(t, "H"+strconv.Itoa(i), "heavy"))
	}
	require.Empty(t, settle(heavy))
	require.Len(t, r.started, 8)
}

// MaxInFlight is a hard per-tenant ceiling even when the pool has room, and
// another tenant is not affected by it.
func TestTenantLimits_MaxInFlightPerTenant(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	r := newInflightRig(t, csms.TenantLimits{
		Tenants:    map[string]csms.TenantLimit{"capped": {MaxInFlight: 2}},
		RejectCode: ocppj.ErrorCodeInternalError,
	})
	var capped []<-chan string
	for i := 0; i < 5; i++ {
		capped = append(capped, r.park(t, "C"+strconv.Itoa(i), "capped"))
	}
	require.Equal(t, repeatStr("InternalError", 3), settle(capped))
	free := []<-chan string{r.park(t, "F1", "free"), r.park(t, "F2", "free"), r.park(t, "F3", "free")}
	require.Empty(t, settle(free))
	require.Len(t, r.started, 5)
	require.Equal(t, 3, r.m.rejections()["capped/inflight"])
}

// A session that closes with calls in flight gives its slots back even though
// the handlers may never finish.
func TestTenantLimits_InFlightReleasedWhenSessionEnds(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	r := newInflightRig(t, csms.TenantLimits{Tenants: map[string]csms.TenantLimit{"a": {MaxInFlight: 1}}})
	c, _, err := dialTenant(r.url, "A1", "a")
	require.NoError(t, err)
	require.NoError(t, c.Write(context.Background(), websocket.MessageText, []byte(`[2,"p1","Heartbeat",{}]`)))
	<-r.started
	require.Equal(t, 1, r.m.inflight("a"))
	require.NoError(t, c.CloseNow())
	// The handler is released by the connection context; either way the slot is back.
	require.Eventually(t, func() bool { return r.m.inflight("a") == 0 }, 3*time.Second, 20*time.Millisecond)
	next := r.park(t, "A2", "a")
	require.Empty(t, settle([]<-chan string{next}))
}
