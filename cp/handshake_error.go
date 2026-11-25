package cp

import (
	"errors"
	"net/http"
)

// HandshakeError is the error Connect (and the reconnect loop's OnDisconnect /
// retry path) returns when the CSMS answered the WebSocket upgrade with an HTTP
// response instead of switching protocols: a refused credential (401/403), an
// unknown charge point id (404), a full server (503 with Retry-After) and so on.
// Its Error text is the underlying error's, so existing log lines and string
// matches are unchanged; use errors.As to read the response.
type HandshakeError struct {
	// StatusCode and Status are the HTTP response of the failed upgrade.
	StatusCode int
	Status     string
	// Header is a copy of the response headers (Retry-After, WWW-Authenticate).
	Header http.Header
	// Err is the underlying dial error.
	Err error
}

func (e *HandshakeError) Error() string { return e.Err.Error() }

func (e *HandshakeError) Unwrap() error { return e.Err }

// Temporary reports whether retrying later may succeed: 408, 425, 429 and 5xx
// (a full or draining server). 401, 403 and 404 are not temporary, a retry with
// the same credentials and id will be refused again.
func (e *HandshakeError) Temporary() bool {
	switch {
	case e.StatusCode == http.StatusRequestTimeout, e.StatusCode == http.StatusTooEarly, e.StatusCode == http.StatusTooManyRequests:
		return true
	case e.StatusCode >= 500:
		return true
	}
	return false
}

// AsHandshakeError finds a *HandshakeError in err's chain.
func AsHandshakeError(err error) (*HandshakeError, bool) {
	var he *HandshakeError
	if errors.As(err, &he) {
		return he, true
	}
	return nil, false
}

func newHandshakeError(resp *http.Response, err error) error {
	if resp == nil || err == nil {
		return err
	}
	return &HandshakeError{StatusCode: resp.StatusCode, Status: resp.Status, Header: resp.Header.Clone(), Err: err}
}
