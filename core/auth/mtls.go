package auth

import (
	"crypto/x509"
	"net/http"
)

// VerifyMTLS checks a client certificate and returns the identity.
type VerifyMTLS func(cpID string, cert *x509.Certificate) (Identity, error)

type mtlsAuth struct{ verify VerifyMTLS }

// MTLSFromClientCert authenticates using the presented TLS client certificate
// (OCPP Security Profile 3). The HTTP server must be configured with
// tls.RequireAndVerifyClientCert.
func MTLSFromClientCert(verify VerifyMTLS) Authenticator { return mtlsAuth{verify: verify} }

func (m mtlsAuth) Authenticate(r *http.Request, cpID string) (Identity, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return Identity{}, ErrUnauthorized
	}
	if cpID == "" {
		cpID = cpIDFromPath(r.URL.Path)
	}
	id, err := m.verify(cpID, r.TLS.PeerCertificates[0])
	if err != nil {
		return Identity{}, err
	}
	if id.Method == "" {
		id.Method = AuthMethodMTLS
	}
	if id.CPID == "" {
		id.CPID = cpID
	}
	return id, nil
}
