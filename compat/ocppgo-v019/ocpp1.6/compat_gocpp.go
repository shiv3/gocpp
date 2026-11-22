// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. See NOTICE.

package ocpp16

import (
	"fmt"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
)

// runHandler runs an application request handler. When the server runs on the
// gocpp runtime the inbound request is already served on a handler goroutine of
// its own, which recovers a panic of the handler and answers the charge point
// with InternalError, so the handler runs there; ocpp-go started a bare goroutine,
// whose panic ends the process.
func (cs *centralSystem) runHandler(f func()) {
	if compatrt.Inline(cs.server) {
		f()
		return
	}
	go f()
}

// runCallback runs a response callback on its own goroutine. On the gocpp
// runtime a panic in it is contained and reported on Errors().
func (cs *centralSystem) runCallback(f func()) {
	if compatrt.Inline(cs.server) {
		go compatrt.Safely(f, func(r any) {
			cs.error(fmt.Errorf("panic in response callback: %v", r))
		})
		return
	}
	go f()
}
