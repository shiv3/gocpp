package csms

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/v16/messages"
	"github.com/shiv3/gocpp/v16/profiles"
	"github.com/stretchr/testify/require"
)

// On records its handler in the *Conn-typed table, so a Server fed by a gateway
// link can run it without a dispatcher.Conn.
func TestOn_RecordsConnTypedHandler(t *testing.T) {
	s := NewServer()
	defer s.Close()
	require.NoError(t, On(s, profiles.Heartbeat, func(ctx context.Context, c *Conn, req messages.HeartbeatRequest) (messages.HeartbeatResponse, error) {
		require.Equal(t, "CP_9", c.ID())
		return messages.HeartbeatResponse{}, nil
	}))
	h, ok := s.handlers.call("Heartbeat")
	require.True(t, ok)
	out, err := h(context.Background(), &Conn{remoteID: "CP_9"}, []byte(`{}`))
	require.NoError(t, err)
	require.NotEmpty(t, out)

	_, ok = s.handlers.call("Nope")
	require.False(t, ok)
	_, ok = s.handlers.send("Nope")
	require.False(t, ok)
}
