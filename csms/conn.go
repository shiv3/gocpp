package csms

import (
	"github.com/shiv3/gocpp/core/dispatcher"
	"github.com/shiv3/gocpp/core/ocppj"
)

// Conn is a CSMS-side connection to a charge point.
type Conn struct {
	inner *dispatcher.Conn
}

// ID returns the charge point identifier.
func (c *Conn) ID() string { return c.inner.ID() }

// Version returns the negotiated OCPP version.
func (c *Conn) Version() ocppj.Version { return c.inner.Version() }
