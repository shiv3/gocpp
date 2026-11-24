// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. See NOTICE.

package ocppj

// Opt-in validation leniency of the gocpp runtime (see compat.SetValidationLeniency).
//
// ocpp-go's validator is the package global Validate, so nothing here touches
// it. Instead, when a message fails validation, the failures are listed and
// handed to a per-server filter; only if the filter tolerates every one of them
// is the message accepted. With no filter installed (the default) none of this
// code runs and the behaviour is ocpp-go v0.19.0's.

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strings"
	"time"

	"gopkg.in/go-playground/validator.v9"

	gdisp "github.com/shiv3/gocpp/core/dispatcher"
	gocppj "github.com/shiv3/gocpp/core/ocppj"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
	"github.com/lorenzodonini/ocpp-go/ocpp"
)

type lenience struct {
	filter       compatrt.ValidationFilter
	specSpelling bool // send OccurenceConstraintViolation (1.6 spec spelling) on 1.6 connections
}

// SetValidation implements compatrt.Control.
func (rt *gocppRuntime) SetValidation(filter compatrt.ValidationFilter, specSpellingV16 bool) {
	if filter == nil && !specSpellingV16 {
		rt.lenient.Store(nil)
		return
	}
	rt.lenient.Store(&lenience{filter: filter, specSpelling: specSpellingV16})
}

// tolerated reports whether err is a validation failure that the installed
// filter accepts in full.
func (rt *gocppRuntime) tolerated(err error, action string) bool {
	l := rt.lenient.Load()
	if l == nil || l.filter == nil {
		return false
	}
	verrs, ok := err.(validator.ValidationErrors)
	if !ok || len(verrs) == 0 {
		return false
	}
	for _, e := range verrs {
		field := e.Namespace()
		if i := strings.Index(field, "Payload."); i >= 0 {
			field = field[i+len("Payload."):]
		}
		if !l.filter(action, field, e.ActualTag()) {
			return false
		}
	}
	return true
}

// parseLenient is Endpoint.ParseMessage for a CALL, except that validation
// failures the filter tolerates do not reject the request and that the opt-in
// decoding options (see decoder) are applied. It returns nil, nil when it has
// nothing to add (no option, request fine or rejected for a reason the filter
// does not cover): the caller then runs ParseMessage, which yields ocpp-go's
// own result and error.
func (rt *gocppRuntime) parseLenient(id, action string, raw interface{}) (Message, error) {
	l := rt.lenient.Load()
	hasFilter := l != nil && l.filter != nil
	if !hasFilter && rt.decode.Load() == nil {
		return nil, nil
	}
	profile, ok := rt.s.GetProfileForFeature(action)
	if !ok {
		return nil, nil
	}
	request, err := profile.ParseRequest(action, raw, rt.requestParser())
	if err != nil {
		return nil, nil
	}
	call := Call{MessageTypeId: CALL, UniqueId: id, Action: action, Payload: request}
	err = Validate.Struct(call)
	if err == nil {
		// Valid once decoded with the opt-in options; ParseMessage would decode
		// the payload without them.
		return &call, nil
	}
	if hasFilter && rt.tolerated(err, action) {
		return &call, nil
	}
	return nil, nil
}

// createCall is Endpoint.CreateCall for an outbound request, except that the
// validation failures the installed filter tolerates do not reject it.
func (rt *gocppRuntime) createCall(s *Server, request ocpp.Request) (*Call, error) {
	call, err := s.CreateCall(request)
	if err == nil {
		return call, nil
	}
	verrs, ok := err.(validator.ValidationErrors)
	if !ok || len(verrs) == 0 || !rt.tolerated(err, request.GetFeatureName()) {
		return nil, err
	}
	return &Call{MessageTypeId: CALL, UniqueId: messageIdGenerator(), Action: request.GetFeatureName(), Payload: request}, nil
}

// decoder holds the opt-in payload decoding options (compat.Leniency.PreserveLargeIntegers,
// AllowNumericSampledValue, LenientDateTime).
type decoder struct {
	compatrt.Decoding
}

// SetDecoding implements compatrt.Control.
func (rt *gocppRuntime) SetDecoding(d compatrt.Decoding) {
	if !d.Any() {
		rt.decode.Store(nil)
		return
	}
	rt.decode.Store(&decoder{d})
}

// decodeRaw is json.Unmarshal into an interface{}; with UseNumber, numbers stay
// json.Number so that big integers are not rounded through float64.
func (rt *gocppRuntime) decodeRaw(b []byte) (interface{}, error) {
	var raw interface{}
	d := rt.decode.Load()
	if d == nil || !d.UseNumber {
		err := json.Unmarshal(b, &raw)
		return raw, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return nil, err
	}
	if dec.More() { // trailing data: json.Unmarshal rejects it too
		return nil, errors.New("invalid character after top-level value")
	}
	return raw, nil
}

func (rt *gocppRuntime) requestParser() func(raw interface{}, t reflect.Type) (ocpp.Request, error) {
	d := rt.decode.Load()
	if d == nil {
		return parseRawJsonRequest
	}
	return func(raw interface{}, t reflect.Type) (ocpp.Request, error) {
		return parseRawJsonRequest(d.coerce(raw, t), t)
	}
}

func (rt *gocppRuntime) confirmationParser() func(raw interface{}, t reflect.Type) (ocpp.Response, error) {
	d := rt.decode.Load()
	if d == nil {
		return parseRawJsonConfirmation
	}
	return func(raw interface{}, t reflect.Type) (ocpp.Response, error) {
		return parseRawJsonConfirmation(d.coerce(raw, t), t)
	}
}

// coerce repairs the decoded payload raw (in place) for the Go type t it is
// about to be decoded into: a number where an OCPP 1.6 SampledValue wants a
// string, and near-RFC3339 timestamps for DateTime fields. Anything it does not
// recognize is left alone, so ocpp-go's own error comes out of the decode.
func (d *decoder) coerce(raw interface{}, t reflect.Type) interface{} {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		if t.Name() == "DateTime" && d.LenientDateTime {
			return repairDateTime(raw, t)
		}
		m, ok := raw.(map[string]interface{})
		if !ok {
			return raw
		}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if f.PkgPath != "" {
				continue
			}
			name := strings.Split(f.Tag.Get("json"), ",")[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = f.Name
			}
			key := name
			if _, ok := m[key]; !ok {
				key = ""
				for k := range m { // encoding/json matches keys case-insensitively
					if strings.EqualFold(k, name) {
						key = k
						break
					}
				}
				if key == "" {
					continue
				}
			}
			v := m[key]
			if d.NumericSampledValue && t.Name() == "SampledValue" && name == "value" && f.Type.Kind() == reflect.String {
				switch n := v.(type) {
				case json.Number:
					m[key] = n.String()
				case float64:
					m[key] = formatFloat(n)
				}
				continue
			}
			m[key] = d.coerce(v, f.Type)
		}
	case reflect.Slice:
		if a, ok := raw.([]interface{}); ok {
			for i := range a {
				a[i] = d.coerce(a[i], t.Elem())
			}
		}
	}
	return raw
}

func formatFloat(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// repairDateTime returns raw unchanged if the DateTime type t accepts it.
func repairDateTime(raw interface{}, t reflect.Type) interface{} {
	str, ok := raw.(string)
	if !ok {
		return raw
	}
	if u, ok := reflect.New(t).Interface().(json.Unmarshaler); ok {
		q, _ := json.Marshal(str)
		if u.UnmarshalJSON(q) == nil {
			return raw
		}
	}
	if fixed, ok := normalizeDateTime(str); ok {
		return fixed
	}
	return raw
}

var (
	looseDateTime = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})(?:[Tt ](\d{2}:\d{2})(?::(\d{2}))?(?:[.,](\d+))?)?\s*([Zz]|[+-]\d{2}(?::?\d{2})?)?$`)
)

// normalizeDateTime rewrites a near-RFC3339 timestamp to RFC3339 (UTC when the
// zone is missing). It returns false when s cannot be repaired.
func normalizeDateTime(s string) (string, bool) {
	m := looseDateTime.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return "", false
	}
	hm, sec := m[2], m[3]
	if hm == "" {
		hm = "00:00"
	}
	if sec == "" {
		sec = "00"
	}
	base := m[1] + "T" + hm + ":" + sec
	if frac := m[4]; frac != "" {
		if len(frac) > 9 {
			frac = frac[:9]
		}
		base += "." + frac
	}
	zone := m[5]
	switch {
	case zone == "":
		zone = "Z"
	case zone == "z":
		zone = "Z"
	case len(zone) == 3:
		zone += ":00"
	case len(zone) == 5:
		zone = zone[:3] + ":" + zone[3:]
	}
	out := base + zone
	if _, err := time.Parse(time.RFC3339Nano, out); err != nil {
		return "", false
	}
	return out, true
}

// wireCode is the CALLERROR code put on the wire for code on dc: ocpp-go
// v0.19.0's spelling, or on OCPP 1.6 the spec's "OccurenceConstraintViolation"
// when compat.Leniency.SpecSpellingOccurenceV16 is set.
func (rt *gocppRuntime) wireCode(dc *gdisp.Conn, code string) string {
	if l := rt.lenient.Load(); l != nil && l.specSpelling && code == string(OccurrenceConstraintViolation) &&
		dc != nil && dc.Subprotocol() == "ocpp1.6" {
		return "OccurenceConstraintViolation"
	}
	return code
}

func (rt *gocppRuntime) errToRaw(dc *gdisp.Conn, e *ocpp.Error) *gdisp.RawCallError {
	return &gdisp.RawCallError{
		Err:  gocppj.NewCallError(gocppj.ErrorCode(e.Code), e.Description, nil),
		Wire: rt.wireCode(dc, string(e.Code)),
	}
}
