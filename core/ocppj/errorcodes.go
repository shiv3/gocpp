package ocppj

// Wire spellings of the error codes each OCPP version defines (OCPP 1.6J
// section 4.2.3, OCPP 2.0.1 / 2.1 Part 4 section 4.3). The two spellings that
// differ between 1.6 and 2.x are FormationViolation / FormatViolation and
// OccurenceConstraintViolation / OccurrenceConstraintViolation.
var (
	errorCodes16 = []string{
		"NotImplemented", "NotSupported", "InternalError", "ProtocolError",
		"SecurityError", "FormationViolation", "PropertyConstraintViolation",
		"OccurenceConstraintViolation", "TypeConstraintViolation", "GenericError",
	}
	errorCodes2x = []string{
		"FormatViolation", "GenericError", "InternalError", "MessageTypeNotSupported",
		"NotImplemented", "NotSupported", "OccurrenceConstraintViolation",
		"PropertyConstraintViolation", "ProtocolError", "RpcFrameworkError",
		"SecurityError", "TypeConstraintViolation",
	}
)

// ErrorCodes returns the wire error codes version v defines, or nil for an
// unknown version. The slice is a copy.
func ErrorCodes(v Version) []string {
	switch v {
	case V16:
		return append([]string(nil), errorCodes16...)
	case V201, V21:
		return append([]string(nil), errorCodes2x...)
	}
	return nil
}

// ValidErrorCode reports whether wire is an error code that version v defines,
// spelled as v spells it (so "FormatViolation" is not valid on 1.6 and
// "OccurenceConstraintViolation" is not valid on 2.x). It is false for an
// unknown version.
func ValidErrorCode(v Version, wire string) bool {
	var set []string
	switch v {
	case V16:
		set = errorCodes16
	case V201, V21:
		set = errorCodes2x
	default:
		return false
	}
	for _, c := range set {
		if c == wire {
			return true
		}
	}
	return false
}
