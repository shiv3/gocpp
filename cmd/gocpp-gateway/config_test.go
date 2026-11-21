package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseConfig_LinkRequiresMutualTLSUnlessInsecure(t *testing.T) {
	_, err := parseConfig([]string{"--id", "g"})
	require.ErrorContains(t, err, "refusing to start the gRPC link without mutual TLS")
	_, err = parseConfig([]string{"--id", "g", "--link-tls-cert", "c", "--link-tls-key", "k"})
	require.ErrorContains(t, err, "refusing to start the gRPC link", "server TLS without client authentication is not enough")
	_, err = parseConfig([]string{"--id", "g", "--link-tls-cert", "c", "--link-tls-key", "k", "--link-client-ca", "ca"})
	require.NoError(t, err)
	c, err := parseConfig([]string{"--id", "g", "--link-insecure"})
	require.NoError(t, err)
	require.True(t, c.LinkInsecure)
	// The NATS link has no listener of ours to secure.
	_, err = parseConfig([]string{"--id", "g", "--link", "nats"})
	require.NoError(t, err)
}

func TestParseConfig_AdminListenerDefaultsToLoopbackAndNeedsTokenBeyond(t *testing.T) {
	c, err := parseConfig([]string{"--id", "g", "--link-insecure"})
	require.NoError(t, err)
	require.Equal(t, "127.0.0.1:9101", c.Admin)
	require.Equal(t, ":9100", c.Metrics)
	for _, addr := range []string{":9101", "0.0.0.0:9101", "10.0.0.5:9101", "[::]:9101"} {
		_, err = parseConfig([]string{"--id", "g", "--link-insecure", "--admin", addr})
		require.ErrorContains(t, err, "without a token", addr)
	}
	for _, addr := range []string{"127.0.0.1:1", "localhost:1", "[::1]:1"} {
		_, err = parseConfig([]string{"--id", "g", "--link-insecure", "--admin", addr})
		require.NoError(t, err, addr)
	}
	tok := filepath.Join(t.TempDir(), "tok")
	require.NoError(t, os.WriteFile(tok, []byte("s3cret\n"), 0o600))
	c, err = parseConfig([]string{"--id", "g", "--link-insecure", "--admin", ":9101", "--admin-token-file", tok})
	require.NoError(t, err)
	require.Equal(t, "s3cret", c.AdminToken)
	t.Setenv("GOCPP_GATEWAY_ADMIN_TOKEN", "fromenv")
	c, err = parseConfig([]string{"--id", "g", "--link-insecure", "--admin", ":9101"})
	require.NoError(t, err)
	require.Equal(t, "fromenv", c.AdminToken)
}

func TestParseConfig_SecretsAreNotFlags(t *testing.T) {
	for _, f := range []string{"--admin-token", "--redis-password", "--attr-source-token"} {
		_, err := parseConfig([]string{"--id", "g", "--link-insecure", f, "x"})
		require.Error(t, err, f)
	}
}

func TestAdminMux_PprofRequiresToken(t *testing.T) {
	m := adminMux("s3cret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for path, want := range map[string]int{"/debug/pprof/": http.StatusOK, "/admin/status": http.StatusNoContent} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		if path == "/debug/pprof/" {
			require.Equal(t, http.StatusUnauthorized, rec.Code, "pprof without the token")
		}
		req = httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer s3cret")
		rec = httptest.NewRecorder()
		m.ServeHTTP(rec, req)
		require.Equal(t, want, rec.Code, path)
	}
}
