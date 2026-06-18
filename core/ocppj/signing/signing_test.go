package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
)

func testECDSACert(t *testing.T) (*ecdsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "cs"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert
}

func TestThumbprint(t *testing.T) {
	_, cert := testECDSACert(t)
	sum := sha256.Sum256(cert.Raw)
	require.Equal(t, base64.RawURLEncoding.EncodeToString(sum[:]), Thumbprint(cert))
}

func TestSignPayloadFlattenedAndHeaders(t *testing.T) {
	key, cert := testECDSACert(t)
	s, err := NewSigner(key, cert)
	require.NoError(t, err)

	signed, err := s.SignPayload("BootNotification", ocppj.Call, []byte(`{"reason":"PowerUp"}`))
	require.NoError(t, err)

	// Flattened JWS JSON shape.
	var flat map[string]string
	require.NoError(t, json.Unmarshal(signed, &flat))
	require.Contains(t, flat, "protected")
	require.Contains(t, flat, "payload")
	require.Contains(t, flat, "signature")
	require.NotContains(t, flat, "signatures")

	// Protected header carries OCPPAction, OCPPMessageTypeId, x5t#S256, alg.
	hdr, err := base64.RawURLEncoding.DecodeString(flat["protected"])
	require.NoError(t, err)
	var ph map[string]any
	require.NoError(t, json.Unmarshal(hdr, &ph))
	require.Equal(t, "BootNotification", ph["OCPPAction"])
	require.EqualValues(t, 2, ph["OCPPMessageTypeId"])
	require.Equal(t, Thumbprint(cert), ph["x5t#S256"])
	require.Equal(t, "ES256", ph["alg"])
}

func TestNewSignerWithAlgorithmRejectsBadAlg(t *testing.T) {
	key, cert := testECDSACert(t)
	_, err := NewSignerWithAlgorithm(key, cert, "HS256")
	require.Error(t, err)
}
