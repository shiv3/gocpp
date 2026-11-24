// Copyright (c) the gocpp authors. Licensed under the MIT License (LICENSE.gocpp).
//
// This file is not part of upstream ocpp-go. See NOTICE.

package ocppj

import "github.com/lorenzodonini/ocpp-go/internal/compatrt"

// StartExternal implements compatrt.Control: Server.Start without listening.
// The application serves the ws.Server's upgrade handler itself (see
// compat.StartWithoutListener); Stop shuts everything down as usual.
func (rt *gocppRuntime) StartExternal(native compatrt.Native) {
	s := rt.s
	s.server.SetCheckClientHandler(s.checkClientHandler)
	s.server.SetNewClientHandler(s.onClientConnected)
	s.server.SetDisconnectedClientHandler(s.onClientDisconnected)
	if rt.native == nil {
		s.server.SetMessageHandler(rt.onMessage)
	}
	rt.start()
	native.PrepareExternal()
}
