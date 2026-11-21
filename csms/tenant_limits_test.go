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
}

func newTenantMetrics() *tenantMetrics {
	return &tenantMetrics{sessions: map[string]int{}, rejected: map[string]int{}}
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
func (m *tenantMetrics) snapshot() (map[string]int, map[string]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, r := map[string]int{}, map[string]int{}
	for k, v := range m.sessions {
		s[k] = v
	}
	for k, v := range m.rejected {
		r[k] = v
	}
	return s, r
}

// tenantServer is an embedded server whose tenant is the X-Tenant header.
func tenantServer(t *testing.T, l csms.TenantLimits, m *tenantMetrics) (*csms.Server, string) {
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
	return srv, "ws" + ts.URL[len("http"):] + "/ocpp/"
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
	_, url := tenantServer(t, csms.TenantLimits{
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

	_, rej := m.snapshot()
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
	_, url := tenantServer(t, csms.TenantLimits{
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

	_, rej := m.snapshot()
	require.Equal(t, rejected, rej["a/messages"], "metrics count every refusal")
	require.Zero(t, rej["b/messages"])
}

// A refused CALL is answered GenericError by default, and RejectCode changes it.
func TestTenantLimits_RejectCode(t *testing.T) {
	t.Cleanup(func() { goleak.VerifyNone(t) })
	_, url := tenantServer(t, csms.TenantLimits{
		Default:    csms.TenantLimit{MessageRate: 1, MessageBurst: 1},
		RejectCode: ocppj.ErrorCodeInternalError,
	}, nil)
	c, _, err := dialTenant(url, "C", "z")
	require.NoError(t, err)
	defer func() { _ = c.CloseNow() }()
	require.Equal(t, "ok", hb(t, c, 1))
	require.Equal(t, "InternalError", hb(t, c, 2))
}
