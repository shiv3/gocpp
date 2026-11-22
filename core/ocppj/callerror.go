package ocppj

import (
	"encoding/json"
	"fmt"
)

// ErrorCode is an OCPP-J CallError code.
type ErrorCode string

const (
	ErrorCodeNotImplemented                ErrorCode = "NotImplemented"
	ErrorCodeNotSupported                  ErrorCode = "NotSupported"
	ErrorCodeInternalError                 ErrorCode = "InternalError"
	ErrorCodeProtocolError                 ErrorCode = "ProtocolError"
	ErrorCodeSecurityError                 ErrorCode = "SecurityError"
	ErrorCodeFormatViolation               ErrorCode = "FormatViolation"    // 2.x
	ErrorCodeFormationViolation            ErrorCode = "FormationViolation" // 1.6
	ErrorCodePropertyConstraintViolation   ErrorCode = "PropertyConstraintViolation"
	ErrorCodeOccurenceConstraintViolation  ErrorCode = "OccurenceConstraintViolation"  //nolint:misspell // OCPP 1.6 spec wire spelling
	ErrorCodeOccurrenceConstraintViolation ErrorCode = "OccurrenceConstraintViolation" // 2.x
	ErrorCodeTypeConstraintViolation       ErrorCode = "TypeConstraintViolation"
	ErrorCodeGenericError                  ErrorCode = "GenericError"
	ErrorCodeMessageTypeNotSupported       ErrorCode = "MessageTypeNotSupported" // 2.x
	ErrorCodeRPCFrameworkError             ErrorCode = "RpcFrameworkError"       // 2.x
)

// CallError is an OCPP-J error response (MessageType 4).
type CallError struct {
	Code        ErrorCode
	Description string
	Details     map[string]any
	// RawDetails is the errorDetails element of the frame as raw JSON, for the
	// cases where it is not (or not only) a JSON object that fits Details. A
	// CallError received from the peer carries it; one that is sent uses it in
	// preference to Details. Nil means no details were received or are to be
	// sent (an empty object goes on the wire).
	RawDetails json.RawMessage
	// IsResultError marks a CallError that arrived as a CALLRESULTERROR (type 5)
	// rather than a CALLERROR (type 4). Wire content is identical; this is for
	// observability only.
	IsResultError bool
	cause         error
}

func (e *CallError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("ocpp call error [%s]: %s", e.Code, e.Description)
	}
	return fmt.Sprintf("ocpp call error [%s]", e.Code)
}

func (e *CallError) Unwrap() error { return e.cause }

// WireCode returns the on-the-wire error code string for version v. OCPP 1.6
// spells two codes differently from 2.x: OccurenceConstraintViolation (sic) and
// FormationViolation, which 2.x renamed FormatViolation. Either spelling is
// mapped to the one the peer's version defines. //nolint:misspell // OCPP 1.6 spec wire spelling
func (e *CallError) WireCode(v Version) string {
	switch e.Code {
	case ErrorCodeOccurrenceConstraintViolation:
		if v == V16 {
			return string(ErrorCodeOccurenceConstraintViolation)
		}
	case ErrorCodeFormationViolation, ErrorCodeFormatViolation:
		if v == V16 {
			return string(ErrorCodeFormationViolation)
		}
		return string(ErrorCodeFormatViolation)
	}
	return string(e.Code)
}

// NewCallError builds a CallError.
func NewCallError(code ErrorCode, desc string, details map[string]any) *CallError {
	return &CallError{Code: code, Description: desc, Details: details}
}

// WrapCallError builds a CallError wrapping an underlying cause.
func WrapCallError(code ErrorCode, cause error, details map[string]any) *CallError {
	desc := ""
	if cause != nil {
		desc = cause.Error()
	}
	return &CallError{Code: code, Description: desc, Details: details, cause: cause}
}
