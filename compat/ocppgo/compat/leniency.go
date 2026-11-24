// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

package compat

import (
	"strings"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
	"github.com/lorenzodonini/ocpp-go/ocppj"
)

// ValidationErrorFilter decides whether one validation failure of an incoming
// message is tolerated. action is the OCPP action ("BootNotification"), field
// the path of the failed field below the payload using Go field names
// ("ConfigurationKey[0].Value", "MeterValue[1].SampledValue") and tag the failed
// ocpp-go validation tag ("required", "min", "max", ...). A message is accepted
// only if the filter tolerates every failure it has. The filter runs on the
// connection goroutines and must be safe for concurrent use.
type ValidationErrorFilter func(action, field, tag string) bool

// Leniency is a preset of the validation quirks of real chargers that ocpp-go
// v0.19.0 rejects. The zero value is ocpp-go v0.19.0's behaviour.
type Leniency struct {
	// AllowEmptyChargePointVendor accepts a BootNotification whose
	// chargePointVendor is empty (upstream: OccurrenceConstraintViolation).
	AllowEmptyChargePointVendor bool
	// AllowLongConfigurationValues accepts a GetConfiguration answer with a
	// configuration key value longer than 500 characters (upstream:
	// PropertyConstraintViolation).
	AllowLongConfigurationValues bool
	// AllowEmptySampledValue accepts a MeterValues request with an empty
	// sampledValue list (upstream: PropertyConstraintViolation, minimum 1). Also
	// applies to OCPP 2.0.1 MeterValues, which has the same constraint.
	AllowEmptySampledValue bool
	// AllowEmptyConfigurationValue accepts an empty ChangeConfiguration value,
	// in a request this server sends (upstream: SendRequest fails with a
	// validation error) and, through the same filter, in any received message
	// that carries one.
	AllowEmptyConfigurationValue bool
	// AllowEmptyIdTag accepts an empty idTag in Authorize and StartTransaction
	// requests received and in RemoteStartTransaction requests sent (upstream:
	// OccurrenceConstraintViolation / validation error).
	AllowEmptyIdTag bool
	// AllowEmptySampledValueValue accepts a sampledValue whose value is the empty
	// string in MeterValues and StopTransaction (upstream: OccurrenceConstraintViolation).
	AllowEmptySampledValueValue bool
	// AllowVendorMeasurands accepts measurand and unit values outside the OCPP 1.6
	// enumerations (vendor extensions, typos such as "Energy.Active.lmport.Register")
	// in MeterValues and StopTransaction. Only OCPP 1.6 messages are affected.
	AllowVendorMeasurands bool
	// AllowNumericSampledValue accepts a JSON number as sampledValue.value of
	// OCPP 1.6 MeterValues and StopTransaction ("value": 12.5 instead of "12.5";
	// upstream: FormatViolation). The number is passed to the handler as the string of its digits.
	AllowNumericSampledValue bool
	// PreserveLargeIntegers decodes the payload numbers of received requests and
	// answers without rounding them through float64, so that an integer up to the
	// int64 range, such as a transactionId of 9223372036854775807, reaches the
	// handler intact (upstream: FormatViolation, the value rounds to 2^63).
	PreserveLargeIntegers bool
	// LenientDateTime repairs near-RFC3339 timestamps in received messages before
	// they are parsed: "2024-01-02 03:04:05", a missing zone (taken as UTC),
	// "+0100" without the colon, a comma as the fractional separator, a date
	// without time. Timestamps that cannot be repaired still reject the message.
	LenientDateTime bool
	// SpecSpellingOccurenceV16 sends the OCPP 1.6 spelling
	// "OccurenceConstraintViolation" in CALLERRORs on OCPP 1.6 connections
	// instead of ocpp-go's "OccurrenceConstraintViolation".
	SpecSpellingOccurenceV16 bool
}

func (l Leniency) filter() ValidationErrorFilter {
	if !l.AllowEmptyChargePointVendor && !l.AllowLongConfigurationValues && !l.AllowEmptySampledValue &&
		!l.AllowEmptyConfigurationValue && !l.AllowEmptyIdTag && !l.AllowEmptySampledValueValue && !l.AllowVendorMeasurands {
		return nil
	}
	return func(action, field, tag string) bool {
		field = stripIndexes(field)
		switch {
		case l.AllowEmptyChargePointVendor && action == "BootNotification" && field == "ChargePointVendor" && tag == "required":
			return true
		case l.AllowLongConfigurationValues && action == "GetConfiguration" && field == "ConfigurationKey.Value" && tag == "max":
			return true
		case l.AllowEmptySampledValue && action == "MeterValues" && field == "MeterValue.SampledValue" && tag == "min":
			return true
		case l.AllowEmptyConfigurationValue && action == "ChangeConfiguration" && field == "Value" && tag == "required":
			return true
		case l.AllowEmptyIdTag && field == "IdTag" && tag == "required" &&
			(action == "Authorize" || action == "StartTransaction" || action == "RemoteStartTransaction"):
			return true
		case l.AllowEmptySampledValueValue && isSampledValueField(action, field, "Value") && tag == "required":
			return true
		case l.AllowVendorMeasurands && isSampledValueField(action, field, "Measurand") && tag == "measurand16":
			return true
		case l.AllowVendorMeasurands && isSampledValueField(action, field, "Unit") && tag == "unitOfMeasure":
			return true
		}
		return false
	}
}

// isSampledValueField reports whether field is the given field of a SampledValue
// of an OCPP 1.6 MeterValues or StopTransaction message (indexes stripped).
func isSampledValueField(action, field, name string) bool {
	switch action {
	case "MeterValues":
		return field == "MeterValue.SampledValue."+name
	case "StopTransaction":
		return field == "TransactionData.SampledValue."+name
	}
	return false
}

func stripIndexes(s string) string {
	for {
		i := strings.IndexByte(s, '[')
		if i < 0 {
			return s
		}
		j := strings.IndexByte(s[i:], ']')
		if j < 0 {
			return s
		}
		s = s[:i] + s[i+j+1:]
	}
}

// SetValidationLeniency applies the preset l to s (a zero Leniency switches
// everything off again). Per server; nothing global changes, and other
// servers and ocpp-go's validator keep upstream behaviour. It may be called
// at any time. It returns false when s runs on the legacy runtime
// (OCPPGO_COMPAT_RUNTIME=legacy, or an injected dispatcher): leniency is not
// supported there and ocpp-go's original validation applies.
func SetValidationLeniency(s *ocppj.Server, l Leniency) bool {
	c, ok := control(s)
	if !ok {
		return false
	}
	c.SetValidation(compatrt.ValidationFilter(l.filter()), l.SpecSpellingOccurenceV16)
	c.SetDecoding(compatrt.Decoding{
		UseNumber:           l.PreserveLargeIntegers,
		NumericSampledValue: l.AllowNumericSampledValue,
		LenientDateTime:     l.LenientDateTime,
	})
	return true
}

// SetValidationErrorFilter installs a custom filter (nil removes it). It
// replaces everything set by SetValidationLeniency, the spelling switch included.
// Same return value and constraints as SetValidationLeniency.
func SetValidationErrorFilter(s *ocppj.Server, f ValidationErrorFilter) bool {
	c, ok := control(s)
	if !ok {
		return false
	}
	c.SetValidation(compatrt.ValidationFilter(f), false)
	c.SetDecoding(compatrt.Decoding{})
	return true
}
