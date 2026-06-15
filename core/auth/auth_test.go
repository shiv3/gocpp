package auth_test

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/stretchr/testify/require"
)

func TestNone_AllowsAll(t *testing.T) {
	id, err := auth.None{}.Authenticate(httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil), "CP_1")
	require.NoError(t, err)
	require.Equal(t, auth.AuthMethodNone, id.Method)
	require.Equal(t, "CP_1", id.CPID)
}

func TestBasicAuth(t *testing.T) {
	a := auth.BasicAuth(func(cpID, password string) (auth.Identity, error) {
		if cpID == "CP_1" && password == "secret" {
			return auth.Identity{CPID: cpID, Method: auth.AuthMethodBasic}, nil
		}
		return auth.Identity{}, auth.ErrUnauthorized
	})

	r := httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil)
	r.SetBasicAuth("CP_1", "secret")
	id, err := a.Authenticate(r, "CP_1")
	require.NoError(t, err)
	require.Equal(t, "CP_1", id.CPID)

	r2 := httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil)
	r2.SetBasicAuth("CP_1", "wrong")
	_, err = a.Authenticate(r2, "CP_1")
	require.ErrorIs(t, err, auth.ErrUnauthorized)

	r3 := httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil)
	_, err = a.Authenticate(r3, "CP_1")
	require.ErrorIs(t, err, auth.ErrUnauthorized)
}

func TestBasicAuth_UsesParsedCPID(t *testing.T) {
	a := auth.BasicAuth(func(cpID, password string) (auth.Identity, error) {
		require.Equal(t, "CP_1", cpID)
		require.Equal(t, "secret", password)
		return auth.Identity{}, nil
	})

	r := httptest.NewRequest(http.MethodGet, "/ocpp/acme/CP_1", nil)
	r.SetBasicAuth("basic-user", "secret")
	id, err := a.Authenticate(r, "CP_1")
	require.NoError(t, err)
	require.Equal(t, "CP_1", id.CPID)
	require.Equal(t, "basic-user", id.Credential)
	require.Equal(t, auth.AuthMethodBasic, id.Method)
}

func TestMTLS_UsesParsedCPID(t *testing.T) {
	cert := &x509.Certificate{}
	a := auth.MTLSFromClientCert(func(cpID string, got *x509.Certificate) (auth.Identity, error) {
		require.Equal(t, "CP_1", cpID)
		require.Same(t, cert, got)
		return auth.Identity{}, nil
	})

	r := httptest.NewRequest(http.MethodGet, "/ocpp/acme/CP_1", nil)
	r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}}
	id, err := a.Authenticate(r, "CP_1")
	require.NoError(t, err)
	require.Equal(t, "CP_1", id.CPID)
	require.Equal(t, auth.AuthMethodMTLS, id.Method)
}
