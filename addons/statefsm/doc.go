// Package statefsm provides a small OCPP 1.6 connector status state machine.
//
// The package is a thin helper around github.com/looplab/fsm. It models the
// connector statuses used by OCPP 1.6 StatusNotification messages and can
// persist the current connector state through a small StateStore interface.
package statefsm
