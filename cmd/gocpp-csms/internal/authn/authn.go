// Package authn builds the charge point authenticators of gocpp-csms serve
// mode: HTTP Basic Auth from a static credentials file (OCPP security profile
// 1/2) and an optional client certificate check (profile 3).
package authn

import (
	"bufio"
	"crypto/subtle"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/shiv3/gocpp/core/auth"
	"golang.org/x/crypto/bcrypt"
)

// Credentials maps a charge point id to its password: plain text, or a
// bcrypt hash ($2a$, $2b$ or $2y$).
type Credentials map[string]string

// LoadFile reads a credentials file: one "cpId:secret" per line; blank lines
// and lines starting with # are ignored. The id is everything before the
// first colon, so the secret may contain colons.
func LoadFile(path string) (Credentials, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	creds := Credentials{}
	sc := bufio.NewScanner(f)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		id, secret, ok := strings.Cut(text, ":")
		if !ok || id == "" || secret == "" {
			return nil, fmt.Errorf("%s:%d: expected cpId:password", path, line)
		}
		if _, dup := creds[id]; dup {
			return nil, fmt.Errorf("%s:%d: duplicate charge point id %q", path, line, id)
		}
		creds[id] = secret
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return creds, nil
}

func isBcrypt(secret string) bool {
	return strings.HasPrefix(secret, "$2a$") || strings.HasPrefix(secret, "$2b$") || strings.HasPrefix(secret, "$2y$")
}

// Verify checks a password for cpID.
func (c Credentials) Verify(cpID, password string) (auth.Identity, error) {
	secret, ok := c[cpID]
	if !ok {
		return auth.Identity{}, auth.ErrUnauthorized
	}
	if isBcrypt(secret) {
		if bcrypt.CompareHashAndPassword([]byte(secret), []byte(password)) != nil {
			return auth.Identity{}, auth.ErrUnauthorized
		}
	} else if subtle.ConstantTimeCompare([]byte(secret), []byte(password)) != 1 {
		return auth.Identity{}, auth.ErrUnauthorized
	}
	return auth.Identity{CPID: cpID, Method: auth.AuthMethodBasic}, nil
}

// Basic returns an authenticator checking HTTP Basic credentials against c.
func Basic(c Credentials) auth.Authenticator {
	return auth.BasicAuth(c.Verify)
}

// CertMatchesCPID returns an authenticator that accepts a verified client
// certificate whose subject common name equals the charge point id.
func CertMatchesCPID() auth.Authenticator {
	return auth.MTLSFromClientCert(func(cpID string, cert *x509.Certificate) (auth.Identity, error) {
		if cert.Subject.CommonName != cpID {
			return auth.Identity{}, auth.ErrUnauthorized
		}
		return auth.Identity{CPID: cpID, Method: auth.AuthMethodMTLS, Credential: cert.Subject.CommonName}, nil
	})
}

// All returns an authenticator that requires every one of as; the identity
// is the first one's, with the metadata of all merged. With no authenticator
// it accepts everything (auth.None).
func All(as ...auth.Authenticator) auth.Authenticator {
	switch len(as) {
	case 0:
		return auth.None{}
	case 1:
		return as[0]
	}
	return allOf(as)
}

type allOf []auth.Authenticator

func (a allOf) Authenticate(r *http.Request, cpID string) (auth.Identity, error) {
	var first auth.Identity
	for i, x := range a {
		id, err := x.Authenticate(r, cpID)
		if err != nil {
			return auth.Identity{}, err
		}
		if i == 0 {
			first = id
			continue
		}
		for k, v := range id.Metadata {
			if first.Metadata == nil {
				first.Metadata = map[string]string{}
			}
			first.Metadata[k] = v
		}
	}
	if first.CPID == "" {
		return auth.Identity{}, errors.New("authn: no charge point id")
	}
	return first, nil
}
