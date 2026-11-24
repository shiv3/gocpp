package csms_test

import (
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/shiv3/gocpp/csms"
	"github.com/stretchr/testify/require"
)

func TestMaxBadFrames_RealSocketClosesWith1008(t *testing.T) {
	r := newRSRig(t, csms.WithMaxBadFrames(3, time.Minute))
	c := r.mustDial(t, "bad")
	r.waitConnected(t, "bad")

	for i := 0; i < 3; i++ {
		require.NoError(t, c.Write(r.ctx, websocket.MessageText, []byte(`this is not OCPP-J`)))
	}
	require.Eventually(t, func() bool {
		_, _, err := c.Read(r.ctx)
		return websocket.CloseStatus(err) == websocket.StatusPolicyViolation
	}, 5*time.Second, 5*time.Millisecond)
	require.Eventually(t, func() bool { _, ok := r.srv.Get("bad"); return !ok }, 2*time.Second, 5*time.Millisecond)
}

func TestMaxBadFrames_DefaultKeepsConnection(t *testing.T) {
	r := newRSRig(t)
	c := r.mustDial(t, "ok")
	r.waitConnected(t, "ok")

	for i := 0; i < 30; i++ {
		require.NoError(t, c.Write(r.ctx, websocket.MessageText, []byte(`this is not OCPP-J`)))
	}
	time.Sleep(200 * time.Millisecond)
	_, ok := r.srv.Get("ok")
	require.True(t, ok)
}
