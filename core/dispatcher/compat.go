package dispatcher

import (
	"errors"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/shiv3/gocpp/core/schema"
)

// OCPPGoErrorCodeMapper returns an ErrorCodeMapper that reproduces the CALLERROR
// codes (and silences) of the ocpp-go library, for staged migrations where
// chargers in the field have been tuned against it:
//
//   - unknown action: NotSupported (gocpp: NotImplemented);
//   - missing required property, empty required array: OccurrenceConstraintViolation,
//     spelled that way on OCPP 1.6 too (the 1.6 wire spelling has one "r");
//   - length / range violations: PropertyConstraintViolation;
//   - an enum value that is not allowed: GenericError;
//   - every other schema violation and every payload that does not decode into
//     the request type (wrong JSON type, bad date-time, not an object):
//     FormationViolation (FormatViolation on 2.x);
//   - an unsupported MessageTypeId: MessageTypeNotSupported, also on 1.6;
//   - a CALL with fewer than three elements or a non-string action: no reply.
//
// It does not touch errors returned by application handlers.
func OCPPGoErrorCodeMapper() ErrorCodeMapper {
	return func(ctx ErrorContext) (string, bool) {
		formation := "FormatViolation"
		if ctx.Version == ocppj.V16 {
			formation = "FormationViolation"
		}
		switch ctx.Source {
		case ErrorSourceUnknownAction:
			return string(ocppj.ErrorCodeNotSupported), true
		case ErrorSourceUnknownMessageType:
			return string(ocppj.ErrorCodeMessageTypeNotSupported), true
		case ErrorSourceDecode:
			return formation, true
		case ErrorSourceMalformedFrame:
			var pe *ocppj.ProtocolError
			if errors.As(ctx.Err, &pe) && (pe.Elements < 3 || pe.BadField == "action") {
				return "", false
			}
			return formation, true
		case ErrorSourceSchema:
			var ve *schema.ViolationError
			if !errors.As(ctx.Err, &ve) {
				return formation, true
			}
			switch ve.Keyword {
			case "required":
				return string(ocppj.ErrorCodeOccurrenceConstraintViolation), true
			case "minLength", "maxLength", "minimum", "maximum", "exclusiveMinimum",
				"exclusiveMaximum", "minItems", "maxItems":
				return string(ocppj.ErrorCodePropertyConstraintViolation), true
			case "enum":
				return string(ocppj.ErrorCodeGenericError), true
			}
			return formation, true
		}
		return ctx.WireCode, ctx.Reply
	}
}
