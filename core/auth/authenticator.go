// Package auth provides connection authentication for the CSMS.
package auth

import (
	"errors"
	"net/http"
)

// ErrUnauthorized indicates authentication failure.
var ErrUnauthorized = errors.New("auth: unauthorized")

// AuthMethod identifies how a connection authenticated.
type AuthMethod string

const (
	AuthMethodNone  AuthMethod = "none"
	AuthMethodBasic AuthMethod = "basic"
	AuthMethodMTLS  AuthMethod = "mtls"
	AuthMethodToken AuthMethod = "token"
)

// Identity is the authenticated identity of a charge point connection.
type Identity struct {
	CPID       string
	Method     AuthMethod
	Credential string
	Metadata   map[string]string
}

// Authenticator authenticates a charge point at WebSocket upgrade time.
type Authenticator interface {
	Authenticate(r *http.Request) (Identity, error)
}

// None accepts every connection (development only).
type None struct{}

// Authenticate always succeeds.
func (None) Authenticate(r *http.Request) (Identity, error) {
	return Identity{CPID: cpIDFromPath(r.URL.Path), Method: AuthMethodNone}, nil
}

func cpIDFromPath(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}
