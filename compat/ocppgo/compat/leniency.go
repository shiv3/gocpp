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
	// SpecSpellingOccurenceV16 sends the OCPP 1.6 spelling
	// "OccurenceConstraintViolation" in CALLERRORs on OCPP 1.6 connections
	// instead of ocpp-go's "OccurrenceConstraintViolation".
	SpecSpellingOccurenceV16 bool
}

func (l Leniency) filter() ValidationErrorFilter {
	if !l.AllowEmptyChargePointVendor && !l.AllowLongConfigurationValues && !l.AllowEmptySampledValue {
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
		}
		return false
	}
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
	return true
}
