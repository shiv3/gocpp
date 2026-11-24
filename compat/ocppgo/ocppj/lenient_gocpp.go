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
	"strings"

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
// failures the filter tolerates do not reject the request. It returns nil, nil
// when it has nothing to add (no filter, request fine or rejected for a reason
// the filter does not cover): the caller then runs ParseMessage, which yields
// ocpp-go's own result and error.
func (rt *gocppRuntime) parseLenient(id, action string, raw interface{}) (Message, error) {
	if l := rt.lenient.Load(); l == nil || l.filter == nil {
		return nil, nil
	}
	profile, ok := rt.s.GetProfileForFeature(action)
	if !ok {
		return nil, nil
	}
	request, err := profile.ParseRequest(action, raw, parseRawJsonRequest)
	if err != nil {
		return nil, nil
	}
	call := Call{MessageTypeId: CALL, UniqueId: id, Action: action, Payload: request}
	if err := Validate.Struct(call); err != nil && rt.tolerated(err, action) {
		return &call, nil
	}
	return nil, nil
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
