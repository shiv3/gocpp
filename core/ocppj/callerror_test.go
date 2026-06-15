package ocppj

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCallError_UnwrapAndIs(t *testing.T) {
	cause := errors.New("boom")
	ce := WrapCallError(ErrorCodeInternalError, cause, nil)
	require.ErrorIs(t, ce, cause)

	var target *CallError
	require.ErrorAs(t, ce, &target)
	require.Equal(t, ErrorCodeInternalError, target.Code)
}

func TestCallError_WireCode_V16Typo(t *testing.T) {
	ce := NewCallError(ErrorCodeOccurrenceConstraintViolation, "x", nil)
	require.Equal(t, "OccurenceConstraintViolation", ce.WireCode(V16))
	require.Equal(t, "OccurrenceConstraintViolation", ce.WireCode(V201))
}
