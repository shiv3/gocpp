package main

import (
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/controlauth"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/hosted"
	"github.com/shiv3/gocpp/cmd/gocpp-csms/internal/msglog"
)

// The hosted test CSMS: message log (--message-log, --message-log-file,
// --record-frames), instance modes and registry (--hosted ...) and the
// control API token (--control-token). All off by default.
func init() {
	serveExtensions = append(serveExtensions,
		controlauth.NewExtension(),
		msglog.NewExtension(),
		hosted.NewExtension(),
	)
}
