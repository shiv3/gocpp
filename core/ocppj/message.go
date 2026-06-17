package ocppj

// Direction indicates which peer originates a given action.
type Direction int

const (
	SentByCP   Direction = iota + 1 // Charge Point initiates (e.g. BootNotification)
	SentByCSMS                      // Central System initiates (e.g. ChangeConfiguration)
	SentByBoth                      // either peer may initiate (e.g. DataTransfer)
)

func (d Direction) String() string {
	switch d {
	case SentByCP:
		return "SentByCP"
	case SentByCSMS:
		return "SentByCSMS"
	case SentByBoth:
		return "SentByBoth"
	default:
		return "UnknownDirection"
	}
}

// Message binds an OCPP action to its request and response types at compile time.
// Generated code creates package-level Message values; user code passes them to
// On/Call so Req/Resp are inferred.
type Message[Req, Resp any] struct {
	Action    string
	Direction Direction
}

// Version identifies an OCPP protocol version.
type Version string

const (
	V16  Version = "1.6"
	V201 Version = "2.0.1"
	V21  Version = "2.1"
)
