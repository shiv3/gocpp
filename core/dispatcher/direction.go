package dispatcher

import "github.com/shiv3/gocpp/core/ocppj"

// Role is the local peer's role.
type Role int

const (
	RoleCSMS Role = iota + 1
	RoleCP
)

// Op is the operation being registered/performed.
type Op int

const (
	OpHandle Op = iota + 1 // registering an inbound handler
	OpCall                 // sending an outbound Call
)

// CheckDirection validates that a role may perform op on a message with the given
// direction. CSMS handles SentByCP and calls SentByCSMS; CP is the mirror.
// Bidirectional messages (SentByBoth, e.g. DataTransfer) may be handled and
// called by either peer, per the OCPP specification.
func CheckDirection(role Role, op Op, dir ocppj.Direction) error {
	if dir == ocppj.SentByBoth {
		return nil
	}
	var want ocppj.Direction
	switch {
	case role == RoleCSMS && op == OpHandle, role == RoleCP && op == OpCall:
		want = ocppj.SentByCP
	case role == RoleCSMS && op == OpCall, role == RoleCP && op == OpHandle:
		want = ocppj.SentByCSMS
	}
	if dir != want {
		return ocppj.ErrInvalidDirection
	}
	return nil
}
