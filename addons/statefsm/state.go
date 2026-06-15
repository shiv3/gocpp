package statefsm

// State is an OCPP 1.6 connector status.
type State string

const (
	StateAvailable     State = "Available"
	StatePreparing     State = "Preparing"
	StateCharging      State = "Charging"
	StateSuspendedEV   State = "SuspendedEV"
	StateSuspendedEVSE State = "SuspendedEVSE"
	StateFinishing     State = "Finishing"
	StateReserved      State = "Reserved"
	StateUnavailable   State = "Unavailable"
	StateFaulted       State = "Faulted"
)

// States lists all OCPP 1.6 connector states modeled by this package.
var States = []State{
	StateAvailable,
	StatePreparing,
	StateCharging,
	StateSuspendedEV,
	StateSuspendedEVSE,
	StateFinishing,
	StateReserved,
	StateUnavailable,
	StateFaulted,
}

// String returns the OCPP status literal.
func (s State) String() string {
	return string(s)
}

// Valid reports whether s is one of the OCPP 1.6 connector statuses.
func (s State) Valid() bool {
	switch s {
	case StateAvailable,
		StatePreparing,
		StateCharging,
		StateSuspendedEV,
		StateSuspendedEVSE,
		StateFinishing,
		StateReserved,
		StateUnavailable,
		StateFaulted:
		return true
	default:
		return false
	}
}

// Event is a named connector state-machine event.
type Event string

const (
	EventPlugIn             Event = "PlugIn"
	EventAuthorize          Event = "Authorize"
	EventStartTransaction   Event = "StartTransaction"
	EventSuspendEV          Event = "SuspendEV"
	EventSuspendEVSE        Event = "SuspendEVSE"
	EventResume             Event = "Resume"
	EventStopTransaction    Event = "StopTransaction"
	EventUnplug             Event = "Unplug"
	EventReserve            Event = "Reserve"
	EventCancelReservation  Event = "CancelReservation"
	EventFault              Event = "Fault"
	EventClearFault         Event = "ClearFault"
	EventChangeAvailability Event = "ChangeAvailability"
)

// Events lists all events accepted by the connector state machine.
var Events = []Event{
	EventPlugIn,
	EventAuthorize,
	EventStartTransaction,
	EventSuspendEV,
	EventSuspendEVSE,
	EventResume,
	EventStopTransaction,
	EventUnplug,
	EventReserve,
	EventCancelReservation,
	EventFault,
	EventClearFault,
	EventChangeAvailability,
}

// String returns the event name used by the FSM.
func (e Event) String() string {
	return string(e)
}

// Valid reports whether e is a known state-machine event.
func (e Event) Valid() bool {
	switch e {
	case EventPlugIn,
		EventAuthorize,
		EventStartTransaction,
		EventSuspendEV,
		EventSuspendEVSE,
		EventResume,
		EventStopTransaction,
		EventUnplug,
		EventReserve,
		EventCancelReservation,
		EventFault,
		EventClearFault,
		EventChangeAvailability:
		return true
	default:
		return false
	}
}
