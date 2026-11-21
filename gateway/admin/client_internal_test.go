package admin

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/require"
)

// Requests share one HTTP client (and so one transport) per TLS config instead
// of building a transport per call (review N10).
func TestDefaultHTTPClientIsReused(t *testing.T) {
	require.Same(t, defaultHTTPClient(nil), defaultHTTPClient(nil))
	cfg := &tls.Config{MinVersion: tls.VersionTLS12}
	require.Same(t, defaultHTTPClient(cfg), defaultHTTPClient(cfg))
	require.NotSame(t, defaultHTTPClient(cfg), defaultHTTPClient(&tls.Config{MinVersion: tls.VersionTLS12}))
	require.NotSame(t, defaultHTTPClient(cfg), defaultHTTPClient(nil))
}
