package csms

import (
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func TestServerExtractCPID_DefaultAndCustom(t *testing.T) {
	srv := NewServer()
	cpID, ok := srv.extractCPID(httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil))
	require.True(t, ok)
	require.Equal(t, "CP_1", cpID)

	_, ok = srv.extractCPID(httptest.NewRequest(http.MethodGet, "/ocpp/acme/CP_1", nil))
	require.False(t, ok)

	srv = NewServer(WithCPIDExtractor(func(r *http.Request) (string, bool) {
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if len(parts) != 3 || parts[0] != "ocpp" {
			return "", false
		}
		return parts[2], true
	}))
	cpID, ok = srv.extractCPID(httptest.NewRequest(http.MethodGet, "/ocpp/acme/CP_1", nil))
	require.True(t, ok)
	require.Equal(t, "CP_1", cpID)
}

func TestServeWSPassesExtractedCPIDToAuthenticator(t *testing.T) {
	a := &recordingAuthenticator{err: auth.ErrUnauthorized}
	srv := NewServer(
		WithAuthenticator(a),
		WithCPIDExtractor(func(r *http.Request) (string, bool) {
			return "CP_1", true
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ocpp/acme/CP_1", nil)
	srv.Handler().ServeHTTP(rec, req)

	require.Equal(t, "CP_1", a.cpID)
	require.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServerDuplicatePolicy_CloseExisting(t *testing.T) {
	srv := NewServer()
	old := newStartedTestConn(t, "CP_1")
	next := newStartedTestConn(t, "CP_1")

	require.True(t, srv.addConn("CP_1", old))
	require.True(t, srv.addConn("CP_1", next))
	got, ok := srv.Get("CP_1")
	require.True(t, ok)
	require.Same(t, next, got)

	select {
	case <-old.inner.Context().Done():
	case <-time.After(time.Second):
		t.Fatal("old connection was not closed")
	}
	require.False(t, srv.removeConn("CP_1", old))
	got, ok = srv.Get("CP_1")
	require.True(t, ok)
	require.Same(t, next, got)
}

func TestServerDuplicatePolicy_RejectNew(t *testing.T) {
	srv := NewServer(WithDuplicatePolicy(DuplicatePolicyRejectNew))
	old := newStartedTestConn(t, "CP_1")
	next := newStartedTestConn(t, "CP_1")

	require.True(t, srv.addConn("CP_1", old))
	require.False(t, srv.addConn("CP_1", next))
	got, ok := srv.Get("CP_1")
	require.True(t, ok)
	require.Same(t, old, got)
	require.NoError(t, next.inner.Close(nil))
}

func TestServeWSRejectsDuplicateBeforeUpgrade(t *testing.T) {
	srv := NewServer(WithDuplicatePolicy(DuplicatePolicyRejectNew))
	existing := newStartedTestConn(t, "CP_1")
	require.True(t, srv.addConn("CP_1", existing))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil)
	srv.Handler().ServeHTTP(rec, req)

	require.Equal(t, http.StatusConflict, rec.Code)
}

func TestConnMetadataAccessors(t *testing.T) {
	header := http.Header{}
	header.Set("X-Trace-ID", "trace-1")
	tlsState := &tls.ConnectionState{ServerName: "csms.example"}
	dconn := dispatcher.NewConn("CP_1", transport.NewFakeWS("ocpp1.6"), dispatcher.DefaultConfig(), dispatcher.NewHandlerRegistry(), dispatcher.ConnMetadata{
		RemoteAddr:    "192.0.2.10:12345",
		RequestHeader: header,
		TLS:           tlsState,
	})
	conn := &Conn{inner: dconn}

	require.Equal(t, "CP_1", conn.ID())
	require.Equal(t, "ocpp1.6", conn.Subprotocol())
	require.Equal(t, "192.0.2.10:12345", conn.RemoteAddr())
	require.Equal(t, "trace-1", conn.RequestHeader().Get("X-Trace-ID"))
	require.Equal(t, "csms.example", conn.TLS().ServerName)
}

type recordingAuthenticator struct {
	cpID string
	err  error
}

func (a *recordingAuthenticator) Authenticate(r *http.Request, cpID string) (auth.Identity, error) {
	a.cpID = cpID
	return auth.Identity{}, a.err
}

func newStartedTestConn(t *testing.T, id string) *Conn {
	t.Helper()
	dconn := dispatcher.NewConn(id, transport.NewFakeWS("ocpp1.6"), dispatcher.DefaultConfig(), dispatcher.NewHandlerRegistry())
	dconn.Start(context.Background())
	t.Cleanup(func() {
		_ = dconn.Close(nil)
	})
	return &Conn{inner: dconn}
}
