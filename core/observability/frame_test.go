package observability_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/shiv3/gocpp/core/observability"
	"github.com/stretchr/testify/require"
)

func TestSlogFrameObserver(t *testing.T) {
	var buf bytes.Buffer
	l := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	obs := observability.SlogFrameObserver(l, slog.LevelDebug, 10)
	obs(context.Background(), observability.FrameEvent{
		CPID: "CP1", Direction: observability.FrameIn, Action: "Heartbeat", MsgID: "m1",
		Raw: []byte(`[2,"m1","Heartbeat",{}]`),
	})
	out := buf.String()
	require.Contains(t, out, "cp_id=CP1")
	require.Contains(t, out, "direction=in")
	require.Contains(t, out, "action=Heartbeat")
	require.Contains(t, out, "msg_id=m1")
	require.Contains(t, out, "truncated=true")
	require.NotContains(t, out, "{}]")
	require.Contains(t, out, "level=DEBUG")

	buf.Reset()
	obs(context.Background(), observability.FrameEvent{Direction: observability.FrameOut, Raw: []byte("x"), Err: errors.New("boom")})
	require.Contains(t, buf.String(), "level=WARN")
	require.Contains(t, buf.String(), "boom")
}
