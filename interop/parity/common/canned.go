package common

import (
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// FixedTime is the currentTime every canned response carries.
const FixedTime = "2026-01-01T00:00:00Z"

// FixedTimeT is FixedTime as a time.Time.
var FixedTimeT = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// BootInterval is the BootNotification heartbeat interval in seconds.
const BootInterval = 300

// PanicMessage is the value the parity.panic handler panics with.
const PanicMessage = "parity: deliberate handler panic"

// ErrorMessage is the text of the application error of parity.error.
const ErrorMessage = "parity: deliberate application error"

// AuthStatus maps an idTag to its authorization status.
func AuthStatus(idTag string) string {
	switch {
	case strings.HasPrefix(idTag, "INVALID"), strings.HasSuffix(idTag, "-INV"):
		return "Invalid"
	case strings.HasPrefix(idTag, "BLOCKED"), strings.HasSuffix(idTag, "-BLK"):
		return "Blocked"
	case strings.HasPrefix(idTag, "EXPIRED"), strings.HasSuffix(idTag, "-EXP"):
		return "Expired"
	}
	return "Accepted"
}

// TxCounter hands out transaction ids starting at 1.
type TxCounter struct{ n atomic.Int32 }

// Next returns the next transaction id.
func (c *TxCounter) Next() int32 { return c.n.Add(1) }

// DataTransfer decision kinds.
const (
	DTReject = "reject"
	DTPanic  = "panic"
	DTSleep  = "sleep"
	DTError  = "error"
	DTEcho   = "echo"
)

// DTDecision tells a DataTransfer handler what to do.
type DTDecision struct {
	Kind  string
	Sleep time.Duration // for DTSleep
}

// DecideDataTransfer applies the vendorId rules. data is the request data as
// decoded by the library (string, number, object...; nil when absent).
func DecideDataTransfer(vendorID string, data any) DTDecision {
	switch vendorID {
	case "parity.reject":
		return DTDecision{Kind: DTReject}
	case "parity.panic":
		return DTDecision{Kind: DTPanic}
	case "parity.error":
		return DTDecision{Kind: DTError}
	case "parity.sleep":
		ms, _ := strconv.Atoi(strings.TrimSpace(fmt.Sprint(data)))
		if ms < 0 {
			ms = 0
		}
		return DTDecision{Kind: DTSleep, Sleep: time.Duration(ms) * time.Millisecond}
	}
	return DTDecision{Kind: DTEcho}
}

// ClassifyReadError normalizes a raw library read/close error string to a
// disconnect cause. Both servers use it so the mapping is identical.
func ClassifyReadError(raw string) string {
	s := strings.ToLower(raw)
	switch {
	case s == "":
		return CauseUnknown
	case strings.Contains(s, "replaced"):
		return CauseReplaced
	case strings.Contains(s, "timeout") || strings.Contains(s, "deadline exceeded"):
		return CauseTimeout
	case strings.Contains(s, "close 1000"), strings.Contains(s, "close 1001"),
		strings.Contains(s, "close 1005"), strings.Contains(s, "close 1006"),
		strings.Contains(s, "statusnormalclosure"), strings.Contains(s, "statusgoingaway"),
		strings.Contains(s, "statusnostatusrcvd"), strings.Contains(s, "eof"),
		strings.Contains(s, "connection reset"):
		return CausePeerClosed
	}
	return CauseReadError
}
