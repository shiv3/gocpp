package auth

import "net/http"

// VerifyBasic checks a charge point id and password, returning the identity.
type VerifyBasic func(cpID, password string) (Identity, error)

type basicAuth struct{ verify VerifyBasic }

// BasicAuth authenticates using HTTP Basic credentials (OCPP Security Profile 1/2).
func BasicAuth(verify VerifyBasic) Authenticator { return basicAuth{verify: verify} }

func (b basicAuth) Authenticate(r *http.Request) (Identity, error) {
	user, pass, ok := r.BasicAuth()
	if !ok {
		return Identity{}, ErrUnauthorized
	}
	id, err := b.verify(user, pass)
	if err != nil {
		return Identity{}, err
	}
	if id.Method == "" {
		id.Method = AuthMethodBasic
	}
	if id.CPID == "" {
		id.CPID = user
	}
	return id, nil
}
