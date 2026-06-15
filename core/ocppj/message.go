package ocppj

// Direction indicates which peer originates a given action.
type Direction int

const (
	SentByCP   Direction = iota + 1 // Charge Point initiates (e.g. BootNotification)
	SentByCSMS                      // Central System initiates (e.g. ChangeConfiguration)
)

func (d Direction) String() string {
	switch d {
	case SentByCP:
		return "SentByCP"
	case SentByCSMS:
		return "SentByCSMS"
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
