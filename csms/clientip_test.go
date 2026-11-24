package csms_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestClientIP(t *testing.T) {
	trusted := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("192.168.1.5/32"), netip.MustParsePrefix("fd00::/8")}
	hdr := func(kv ...string) http.Header {
		h := http.Header{}
		for i := 0; i < len(kv); i += 2 {
			h.Add(kv[i], kv[i+1])
		}
		return h
	}
	cases := []struct {
		name    string
		remote  string
		h       http.Header
		trusted []netip.Prefix
		want    string
	}{
		{"no trusted proxies ignores headers", "203.0.113.9:5555", hdr("X-Forwarded-For", "1.2.3.4"), nil, "203.0.113.9"},
		{"untrusted peer cannot spoof", "203.0.113.9:5555", hdr("X-Forwarded-For", "1.2.3.4", "X-Real-IP", "1.2.3.4"), trusted, "203.0.113.9"},
		{"trusted peer, single hop", "10.1.1.1:80", hdr("X-Forwarded-For", "198.51.100.7"), trusted, "198.51.100.7"},
		{"rightmost untrusted wins over spoofed left", "10.1.1.1:80", hdr("X-Forwarded-For", "6.6.6.6, 198.51.100.7, 10.2.2.2"), trusted, "198.51.100.7"},
		{"split across header lines", "10.1.1.1:80", hdr("X-Forwarded-For", "6.6.6.6", "X-Forwarded-For", "198.51.100.7, 192.168.1.5"), trusted, "198.51.100.7"},
		{"all trusted gives leftmost", "10.1.1.1:80", hdr("X-Forwarded-For", "10.9.9.9, 10.8.8.8"), trusted, "10.9.9.9"},
		{"garbage entry stops the walk", "10.1.1.1:80", hdr("X-Forwarded-For", "198.51.100.7, bogus, 10.2.2.2"), trusted, "10.2.2.2"},
		{"x-real-ip without xff", "10.1.1.1:80", hdr("X-Real-IP", "198.51.100.8"), trusted, "198.51.100.8"},
		{"invalid x-real-ip", "10.1.1.1:80", hdr("X-Real-IP", "nope"), trusted, "10.1.1.1"},
		{"xff beats x-real-ip", "10.1.1.1:80", hdr("X-Forwarded-For", "198.51.100.7", "X-Real-IP", "198.51.100.8"), trusted, "198.51.100.7"},
		{"ipv6 peer and client", "[fd00::1]:443", hdr("X-Forwarded-For", "2001:db8::7"), trusted, "2001:db8::7"},
		{"ipv4-mapped peer", "[::ffff:10.1.1.1]:80", hdr("X-Forwarded-For", "198.51.100.7"), trusted, "198.51.100.7"},
		{"bare host remote", "203.0.113.9", nil, trusted, "203.0.113.9"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, csms.ClientIP(tc.remote, tc.h, tc.trusted))
		})
	}
}

func clientIPViaServer(t *testing.T, header http.Header, opts ...csms.Option) string {
	t.Helper()
	got := make(chan string, 1)
	srv := csms.NewServer(append([]csms.Option{csms.WithSubProtocols("ocpp1.6"), csms.WithOnConnect(func(c *csms.Conn) { got <- c.ClientIP() })}, opts...)...)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	header.Set("Sec-Dummy", "1")
	ws, _, err := websocket.Dial(ctx, "ws"+ts.URL[len("http"):]+"/ocpp/CP", &websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}, HTTPHeader: header})
	require.NoError(t, err)
	defer func() { _ = ws.Close(websocket.StatusNormalClosure, "") }()
	select {
	case ip := <-got:
		return ip
	case <-ctx.Done():
		t.Fatal("no connect callback")
		return ""
	}
}

// Default (no option): the headers are never believed.
func TestConnClientIP_DefaultIsRemoteAddr(t *testing.T) {
	h := http.Header{}
	h.Set("X-Forwarded-For", "198.51.100.7")
	require.Equal(t, "127.0.0.1", clientIPViaServer(t, h))
}

func TestConnClientIP_TrustedProxy(t *testing.T) {
	h := http.Header{}
	h.Set("X-Forwarded-For", "6.6.6.6, 198.51.100.7")
	require.Equal(t, "198.51.100.7", clientIPViaServer(t, h, csms.WithTrustedProxies(netip.MustParsePrefix("127.0.0.0/8"))))
	// A peer outside the trusted set is not believed.
	require.Equal(t, "127.0.0.1", clientIPViaServer(t, h, csms.WithTrustedProxies(netip.MustParsePrefix("10.0.0.0/8"))))
}
