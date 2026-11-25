package ocppj

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidErrorCode(t *testing.T) {
	require.True(t, ValidErrorCode(V16, "FormationViolation"))
	require.False(t, ValidErrorCode(V16, "FormatViolation"))
	require.True(t, ValidErrorCode(V16, "OccurenceConstraintViolation"))
	require.False(t, ValidErrorCode(V16, "OccurrenceConstraintViolation"))
	require.False(t, ValidErrorCode(V16, "RpcFrameworkError"))
	require.False(t, ValidErrorCode(V16, "MessageTypeNotSupported"))
	for _, v := range []Version{V201, V21} {
		require.True(t, ValidErrorCode(v, "FormatViolation"))
		require.False(t, ValidErrorCode(v, "FormationViolation"))
		require.True(t, ValidErrorCode(v, "OccurrenceConstraintViolation"))
		require.False(t, ValidErrorCode(v, "OccurenceConstraintViolation"))
		require.True(t, ValidErrorCode(v, "RpcFrameworkError"))
		require.True(t, ValidErrorCode(v, "MessageTypeNotSupported"))
		require.False(t, ValidErrorCode(v, "Bogus"))
	}
	require.False(t, ValidErrorCode("nope", "GenericError"))
	require.Nil(t, ErrorCodes("nope"))
	require.Len(t, ErrorCodes(V16), 10)
	require.Len(t, ErrorCodes(V21), 12)
}

// Every code a CallError can carry is valid on the wire for each version once
// WireCode has spelled it for that version.
func TestWireCodeIsValidForVersion(t *testing.T) {
	codes16 := []ErrorCode{ErrorCodeNotImplemented, ErrorCodeNotSupported, ErrorCodeInternalError, ErrorCodeProtocolError,
		ErrorCodeSecurityError, ErrorCodeFormationViolation, ErrorCodePropertyConstraintViolation,
		ErrorCodeOccurrenceConstraintViolation, ErrorCodeTypeConstraintViolation, ErrorCodeGenericError}
	for _, c := range codes16 {
		require.True(t, ValidErrorCode(V16, NewCallError(c, "", nil).WireCode(V16)), string(c))
	}
	for _, c := range append(codes16, ErrorCodeMessageTypeNotSupported, ErrorCodeRPCFrameworkError, ErrorCodeFormatViolation) {
		require.True(t, ValidErrorCode(V201, NewCallError(c, "", nil).WireCode(V201)), string(c))
	}
}
