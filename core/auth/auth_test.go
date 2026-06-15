package auth_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/shiv3/gocpp/core/auth"
	"github.com/stretchr/testify/require"
)

func TestNone_AllowsAll(t *testing.T) {
	id, err := auth.None{}.Authenticate(httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil))
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
	id, err := a.Authenticate(r)
	require.NoError(t, err)
	require.Equal(t, "CP_1", id.CPID)

	r2 := httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil)
	r2.SetBasicAuth("CP_1", "wrong")
	_, err = a.Authenticate(r2)
	require.ErrorIs(t, err, auth.ErrUnauthorized)

	r3 := httptest.NewRequest(http.MethodGet, "/ocpp/CP_1", nil)
	_, err = a.Authenticate(r3)
	require.ErrorIs(t, err, auth.ErrUnauthorized)
}
