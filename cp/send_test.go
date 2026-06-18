package cp

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
)

var notifyReport = ocppj.SendMessage[map[string]any]{
	Action:    "NotifyReport",
	Direction: ocppj.SentByCP,
}

var csmsTriggered = ocppj.SendMessage[map[string]any]{
	Action:    "CostUpdated",
	Direction: ocppj.SentByCSMS,
}

// TestCP_OnSend_WrongDirection asserts that a CP may not register a handler for
// a SentByCP SEND (CP cannot handle something it sends itself).
func TestCP_OnSend_WrongDirection(t *testing.T) {
	c := NewClient("CP_1", "ws://x")
	// CP can handle SentByCSMS messages.
	err := OnSend(c, csmsTriggered, func(_ context.Context, _ map[string]any) error {
		return nil
	})
	require.NoError(t, err)

	// CP cannot handle SentByCP (it sends those, doesn't receive them).
	err = OnSend(c, notifyReport, func(_ context.Context, _ map[string]any) error {
		return nil
	})
	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

// TestCP_Send_WrongDirection asserts that a CP cannot send a SentByCSMS message.
func TestCP_Send_WrongDirection(t *testing.T) {
	c := NewClient("CP_1", "ws://x")
	err := Send(context.Background(), c, csmsTriggered, map[string]any{})
	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}

// TestCP_Send_NotConnected asserts that sending a SentByCP message on a
// disconnected client returns ErrNotConnected (direction passes, conn is nil).
func TestCP_Send_NotConnected(t *testing.T) {
	c := NewClient("CP_1", "ws://x")
	err := Send(context.Background(), c, notifyReport, map[string]any{})
	require.ErrorIs(t, err, ocppj.ErrNotConnected)
}
