package csms_test

import (
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func dialStatus(t *testing.T, srv *csms.Server, id string) int {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c, resp, err := websocket.Dial(t.Context(), "ws"+ts.URL[len("http"):]+"/ocpp/"+id,
		&websocket.DialOptions{Subprotocols: []string{"ocpp1.6"}})
	if err == nil {
		_ = c.CloseNow()
		return http.StatusSwitchingProtocols
	}
	require.NotNil(t, resp)
	return resp.StatusCode
}

func TestCPIDPolicy_DefaultAcceptsLongAndOddIDs(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"))
	defer srv.Close()
	require.Equal(t, http.StatusSwitchingProtocols, dialStatus(t, srv, strings.Repeat("a", 200)))
	require.Equal(t, http.StatusSwitchingProtocols, dialStatus(t, srv, "CP%20with%20space"))
}

func TestCPIDPolicy_MaxLength(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithMaxCPIDLength(8))
	defer srv.Close()
	require.Equal(t, http.StatusSwitchingProtocols, dialStatus(t, srv, "12345678"))
	require.Equal(t, http.StatusBadRequest, dialStatus(t, srv, "123456789"))
}

func TestCPIDPolicy_IdentifierStringPattern(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithCPIDPattern(csms.IdentifierStringPattern))
	defer srv.Close()
	require.Equal(t, http.StatusSwitchingProtocols, dialStatus(t, srv, "CP-1_a.b:c=d+e|f@g*h"))
	require.Equal(t, http.StatusBadRequest, dialStatus(t, srv, "CP%20space"))
	require.Equal(t, http.StatusBadRequest, dialStatus(t, srv, "CP;drop"))
	require.Equal(t, http.StatusBadRequest, dialStatus(t, srv, strings.Repeat("a", 49)))
	require.Equal(t, http.StatusSwitchingProtocols, dialStatus(t, srv, strings.Repeat("a", 48)))
}

func TestCPIDPolicy_CustomPattern(t *testing.T) {
	srv := csms.NewServer(csms.WithSubProtocols("ocpp1.6"), csms.WithCPIDPattern(regexp.MustCompile(`^CP\d+$`)))
	defer srv.Close()
	require.Equal(t, http.StatusSwitchingProtocols, dialStatus(t, srv, "CP12"))
	require.Equal(t, http.StatusBadRequest, dialStatus(t, srv, "XX12"))
}
