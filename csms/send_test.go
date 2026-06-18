package csms

import (
	"context"
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
)

var notifyPeriodic = ocppj.SendMessage[map[string]any]{
	Action:    "NotifyPeriodicEventStream",
	Direction: ocppj.SentByCP,
}

func TestCSMSOnSendRegisters(t *testing.T) {
	s := NewServer(WithSubProtocols("ocpp2.1"))
	err := OnSend(s, notifyPeriodic, func(_ context.Context, _ *Conn, _ map[string]any) error {
		return nil
	})
	require.NoError(t, err)
}

func TestCSMSSendWrongDirection(t *testing.T) {
	// CSMS cannot send a SentByCP message — direction check fires before conn is touched.
	c := newStartedTestConn(t)
	err := Send(context.Background(), c, notifyPeriodic, map[string]any{})
	require.ErrorIs(t, err, ocppj.ErrInvalidDirection)
}
