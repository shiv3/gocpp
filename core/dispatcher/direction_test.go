package dispatcher

import (
	"testing"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
)

func TestCheckDirection(t *testing.T) {
	// CSMS may handle SentByCP messages and Call SentByCSMS messages.
	require.NoError(t, CheckDirection(RoleCSMS, OpHandle, ocppj.SentByCP))
	require.NoError(t, CheckDirection(RoleCSMS, OpCall, ocppj.SentByCSMS))
	require.ErrorIs(t, CheckDirection(RoleCSMS, OpCall, ocppj.SentByCP), ocppj.ErrInvalidDirection)

	// CP may handle SentByCSMS messages and Call SentByCP messages.
	require.NoError(t, CheckDirection(RoleCP, OpHandle, ocppj.SentByCSMS))
	require.NoError(t, CheckDirection(RoleCP, OpCall, ocppj.SentByCP))
	require.ErrorIs(t, CheckDirection(RoleCP, OpHandle, ocppj.SentByCP), ocppj.ErrInvalidDirection)
}
