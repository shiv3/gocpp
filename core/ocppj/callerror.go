package ocppj

import "fmt"

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
	cause       error
}

func (e *CallError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("ocpp call error [%s]: %s", e.Code, e.Description)
	}
	return fmt.Sprintf("ocpp call error [%s]", e.Code)
}

func (e *CallError) Unwrap() error { return e.cause }

// WireCode returns the on-the-wire error code string, applying the OCPP 1.6
// spec's misspelled OccurenceConstraintViolation code for 1.6 connections. //nolint:misspell // OCPP 1.6 spec wire spelling
func (e *CallError) WireCode(v Version) string {
	if e.Code == ErrorCodeOccurrenceConstraintViolation && v == V16 {
		return string(ErrorCodeOccurenceConstraintViolation)
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
