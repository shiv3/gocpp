package csms_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestInboundRateLimit_RealSocketRejects(t *testing.T) {
	r := newRSRig(t, csms.WithInboundRateLimit(0.001, 2))
	c := r.mustDial(t, "rl")
	r.waitConnected(t, "rl")

	limited := 0
	for i := 0; i < 6; i++ {
		require.NoError(t, c.Write(r.ctx, websocket.MessageText, []byte(fmt.Sprintf(`[2,"id%d","Heartbeat",{}]`, i))))
		_, data, err := c.Read(r.ctx)
		require.NoError(t, err)
		var arr []any
		require.NoError(t, json.Unmarshal(data, &arr))
		if len(arr) > 2 && arr[0].(float64) == 4 && arr[2] == "GenericError" {
			limited++
		}
	}
	require.Equal(t, 4, limited, "burst of 2 passes, the rest is refused")
	_, ok := r.srv.Get("rl")
	require.True(t, ok, "connection stays up")
}

func TestInboundRateLimit_RealSocketClose(t *testing.T) {
	r := newRSRig(t, csms.WithInboundRateLimit(0.001, 1), csms.WithInboundRateLimitAction(dispatcher.RateLimitClose))
	c := r.mustDial(t, "rc")
	r.waitConnected(t, "rc")

	for i := 0; i < 3; i++ {
		require.NoError(t, c.Write(r.ctx, websocket.MessageText, []byte(fmt.Sprintf(`[2,"id%d","Heartbeat",{}]`, i))))
	}
	require.Eventually(t, func() bool {
		_, _, err := c.Read(r.ctx)
		return websocket.CloseStatus(err) == websocket.StatusPolicyViolation
	}, 5*time.Second, 5*time.Millisecond)
}
