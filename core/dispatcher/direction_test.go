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

func TestCheckDirection_Bidirectional(t *testing.T) {
	// Bidirectional messages (e.g. DataTransfer) may be handled and called by
	// either peer, per the OCPP specification.
	for _, role := range []Role{RoleCSMS, RoleCP} {
		for _, op := range []Op{OpHandle, OpCall} {
			require.NoError(t, CheckDirection(role, op, ocppj.SentByBoth))
		}
	}
}
