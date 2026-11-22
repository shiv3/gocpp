// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).

// Package compat holds the switches of the gocpp compatibility module that have
// no counterpart in ocpp-go. It is the only package of the module that is not in
// ocpp-go, so that every ocpp-go package keeps exactly its upstream API.
//
// The runtime is selected with the environment variable OCPPGO_COMPAT_RUNTIME:
//
//	unset    the gocpp runtime for servers built without a custom ServerDispatcher
//	gocpp    the gocpp runtime also for an injected *ocppj.DefaultServerDispatcher
//	legacy   ocpp-go's original code path everywhere (operational fallback)
package compat

import (
	"time"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
	"github.com/lorenzodonini/ocpp-go/ocppj"
)

// Runtime reports which runtime serves s: "gocpp" or "legacy".
func Runtime(s *ocppj.Server) string {
	if _, ok := compatrt.Get(s, compatrt.KindRuntime); ok {
		return "gocpp"
	}
	return "legacy"
}

// SetCallTimeout sets how long an outbound request waits for its answer on a
// server that runs on the gocpp runtime, as ocppj.DefaultServerDispatcher's
// SetTimeout does for the original one (without injecting a dispatcher). Call
// it before Start. It returns false when s runs on the legacy runtime.
func SetCallTimeout(s *ocppj.Server, d time.Duration) bool {
	v, ok := compatrt.Get(s, compatrt.KindRuntime)
	if !ok {
		return false
	}
	c, ok := v.(compatrt.Control)
	if !ok {
		return false
	}
	c.SetCallTimeout(d)
	return true
}
