package signing

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"testing"
	"time"

	"github.com/shiv3/gocpp/core/ocppj"
	"github.com/stretchr/testify/require"
)

func TestVerifyRoundTrip(t *testing.T) {
	key, cert := testECDSACert(t)
	s, err := NewSigner(key, cert)
	require.NoError(t, err)
	signed, err := s.SignPayload("BootNotification", ocppj.Call, []byte(`{"reason":"PowerUp"}`))
	require.NoError(t, err)

	v := NewVerifier(cert)
	payload, hdr, err := v.VerifyPayload(signed, "BootNotification", ocppj.Call)
	require.NoError(t, err)
	require.JSONEq(t, `{"reason":"PowerUp"}`, string(payload))
	require.Equal(t, "ES256", hdr.Alg)
	require.Equal(t, "BootNotification", hdr.OCPPAction)
	require.Equal(t, 2, hdr.OCPPMessageTypeID)
	require.Equal(t, Thumbprint(cert), hdr.X5tS256)
}

func TestVerifyRejectsTamperedPayload(t *testing.T) {
	key, cert := testECDSACert(t)
	s, _ := NewSigner(key, cert)
	signed, _ := s.SignPayload("Authorize", ocppj.Call, []byte(`{"idToken":{"idToken":"A","type":"ISO14443"}}`))
	var flat map[string]string
	require.NoError(t, json.Unmarshal(signed, &flat))
	flat["payload"] = flat["payload"][:len(flat["payload"])-2] + "AA" // corrupt
	bad, _ := json.Marshal(flat)
	v := NewVerifier(cert)
	_, _, err := v.VerifyPayload(bad, "", 0)
	require.Error(t, err)
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	key, cert := testECDSACert(t)
	s, _ := NewSigner(key, cert)
	signed, _ := s.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	otherKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	otherTmpl := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "other"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, _ := x509.CreateCertificate(rand.Reader, otherTmpl, otherTmpl, &otherKey.PublicKey, otherKey)
	otherCert, _ := x509.ParseCertificate(der)
	v := NewVerifier(otherCert)
	_, _, err := v.VerifyPayload(signed, "", 0)
	require.Error(t, err)
}

func TestVerifyRejectsHeaderMismatch(t *testing.T) {
	key, cert := testECDSACert(t)
	s, _ := NewSigner(key, cert)
	signed, _ := s.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	v := NewVerifier(cert)
	_, _, err := v.VerifyPayload(signed, "BootNotification", ocppj.Call) // action mismatch
	require.Error(t, err)
}

func TestUnwrapPayloadNoVerify(t *testing.T) {
	key, cert := testECDSACert(t)
	s, _ := NewSigner(key, cert)
	signed, _ := s.SignPayload("Heartbeat", ocppj.Call, []byte(`{"k":1}`))
	payload, err := UnwrapPayload(signed)
	require.NoError(t, err)
	require.JSONEq(t, `{"k":1}`, string(payload))
}

func testRSACert(t *testing.T) (*rsa.PrivateKey, *x509.Certificate) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "rsa-cs"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return key, cert
}

func TestVerifyRoundTripRS256(t *testing.T) {
	key, cert := testRSACert(t)
	s, err := NewSigner(key, cert) // RSA -> RS256
	require.NoError(t, err)
	signed, err := s.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	require.NoError(t, err)
	payload, hdr, err := NewVerifier(cert).VerifyPayload(signed, "Heartbeat", ocppj.Call)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(payload))
	require.Equal(t, "RS256", hdr.Alg)
}

func TestVerifyRoundTripRS384(t *testing.T) {
	key, cert := testRSACert(t)
	s, err := NewSignerWithAlgorithm(key, cert, "RS384")
	require.NoError(t, err)
	signed, err := s.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	require.NoError(t, err)
	payload, hdr, err := NewVerifier(cert).VerifyPayload(signed, "Heartbeat", ocppj.Call)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(payload))
	require.Equal(t, "RS384", hdr.Alg)
}

func TestVerifyRejectsUntrustedThumbprint(t *testing.T) {
	key, cert := testECDSACert(t)
	s, _ := NewSigner(key, cert)
	signed, _ := s.SignPayload("Heartbeat", ocppj.Call, []byte(`{}`))
	v := NewVerifier() // trusts nothing
	_, _, err := v.VerifyPayload(signed, "", 0)
	require.Error(t, err)
}
