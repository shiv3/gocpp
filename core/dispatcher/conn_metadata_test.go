package dispatcher

import (
	"crypto/tls"
	"net/http"
	"testing"

	"github.com/shiv3/gocpp/core/transport"
	"github.com/stretchr/testify/require"
)

func TestConnMetadata(t *testing.T) {
	header := http.Header{}
	header.Set("X-Trace-ID", "trace-1")
	tlsState := &tls.ConnectionState{ServerName: "csms.example"}

	c := NewConn("CP_1", transport.NewFakeWS("ocpp1.6"), DefaultConfig(), NewHandlerRegistry(), ConnMetadata{
		RemoteAddr:    "192.0.2.10:12345",
		RequestHeader: header,
		TLS:           tlsState,
	})

	header.Set("X-Trace-ID", "changed")
	tlsState.ServerName = "changed"

	require.Equal(t, "192.0.2.10:12345", c.RemoteAddr())
	require.Equal(t, "trace-1", c.RequestHeader().Get("X-Trace-ID"))
	require.Equal(t, "csms.example", c.TLS().ServerName)

	gotHeader := c.RequestHeader()
	gotHeader.Set("X-Trace-ID", "mutated")
	require.Equal(t, "trace-1", c.RequestHeader().Get("X-Trace-ID"))

	gotTLS := c.TLS()
	gotTLS.ServerName = "mutated"
	require.Equal(t, "csms.example", c.TLS().ServerName)
}
