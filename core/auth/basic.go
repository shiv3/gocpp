package auth

import "net/http"

// VerifyBasic checks the parsed charge point id and password, returning the identity.
type VerifyBasic func(cpID, password string) (Identity, error)

type basicAuth struct{ verify VerifyBasic }

// BasicAuth authenticates using HTTP Basic credentials (OCPP Security Profile 1/2).
// The verifier receives the charge point id parsed from the request path. If
// the returned identity leaves Credential empty, it is set to the HTTP Basic
// username.
func BasicAuth(verify VerifyBasic) Authenticator { return basicAuth{verify: verify} }

func (b basicAuth) Authenticate(r *http.Request, cpID string) (Identity, error) {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return Identity{}, ErrUnauthorized
	}
	if cpID == "" {
		cpID = user
	}
	id, err := b.verify(cpID, pass)
	if err != nil {
		return Identity{}, err
	}
	if id.Method == "" {
		id.Method = AuthMethodBasic
	}
	if id.CPID == "" {
		id.CPID = cpID
	}
	if id.Credential == "" {
		id.Credential = user
	}
	return id, nil
}
