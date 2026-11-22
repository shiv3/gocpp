// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. See NOTICE.

package ocpp2

import (
	"fmt"

	"github.com/lorenzodonini/ocpp-go/internal/compatrt"
)

// runHandler runs an application request handler; see ocpp1.6/compat_gocpp.go.
func (cs *csms) runHandler(f func()) {
	if compatrt.Inline(cs.server) {
		f()
		return
	}
	go f()
}

// runCallback runs a response callback on its own goroutine; see ocpp1.6/compat_gocpp.go.
func (cs *csms) runCallback(f func()) {
	if compatrt.Inline(cs.server) {
		go compatrt.Safely(f, func(r any) {
			cs.error(fmt.Errorf("panic in response callback: %v", r))
		})
		return
	}
	go f()
}
