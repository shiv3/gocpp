package transport

import (
	"errors"
	"fmt"
	"testing"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

type fakeCause struct{}

func (fakeCause) Error() string                   { return "cause" }
func (fakeCause) CloseCode() (StatusCode, string) { return StatusPolicyViolation, "x" }

func TestAsCloseError(t *testing.T) {
	ce, ok := AsCloseError(fmt.Errorf("read: %w", websocket.CloseError{Code: websocket.StatusNormalClosure, Reason: "bye"}))
	require.True(t, ok)
	require.Equal(t, StatusNormalClosure, ce.Code)
	require.Equal(t, "bye", ce.Reason)
	require.True(t, ce.Normal())
	require.Contains(t, ce.Error(), "1000")

	ce, ok = AsCloseError(fmt.Errorf("wrapped: %w", fakeCause{}))
	require.True(t, ok)
	require.Equal(t, StatusPolicyViolation, ce.Code)
	require.False(t, ce.Normal())

	_, ok = AsCloseError(errors.New("i/o timeout"))
	require.False(t, ok)
	_, ok = AsCloseError(nil)
	require.False(t, ok)

	for code, want := range map[StatusCode]bool{1000: true, 1001: true, 1005: true, 1006: false, 1008: false, 1009: false, 1012: false} {
		require.Equal(t, want, (&CloseError{Code: code}).Normal(), "code %d", code)
	}
}
